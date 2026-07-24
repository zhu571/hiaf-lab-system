"""
HiafStorage — InfluxDB + SQLite persistence for HIAF Gas Cell IOC.

- maybe_write_sensors: batch-write sensor values to SQLite on threshold change or periodic flush.
- maybe_write_influx:  batch-write sensor + PI control + pump data to InfluxDB.
"""

from __future__ import annotations

import asyncio
import logging
import time

import aiosqlite
from influxdb_client import InfluxDBClient, Point
from influxdb_client.client.write_api import SYNCHRONOUS

LOGGER = logging.getLogger(__name__)

INFLUX_WRITE_SEC = 10.0
SQLITE_FLUSH_SEC = 30.0
SENSOR_CHANGE_REL = 0.005
SENSOR_CHANGE_ABS = 0.1


class HiafStorage:
    def __init__(
        self,
        influx_url: str,
        influx_token: str,
        influx_org: str,
        influx_bucket: str,
        db_path: str,
        sensor_tags: list[tuple[str, str]],
        pump_tags: list[tuple[str, str, str]],
    ) -> None:
        self._sensor_tags = sensor_tags
        self._pump_tags = pump_tags

        self._influx_bucket = influx_bucket
        self._influx_org = influx_org

        self._influx_write_api = None
        self._last_influx_write = 0.0
        self._pending_influx_points: list = []
        self._pending_influx_batches = 0
        self._MAX_PENDING_BATCHES = 2
        try:
            client = InfluxDBClient(
                url=influx_url, token=influx_token, org=influx_org)
            self._influx_write_api = client.write_api(
                write_options=SYNCHRONOUS)
            LOGGER.info("InfluxDB connected — %s", influx_url)
        except Exception as e:
            LOGGER.warning("InfluxDB init failed: %s", e)

        self._db_path = str(db_path)
        self._db: aiosqlite.Connection | None = None
        self._last_db_write = 0.0
        self._last_written_values: dict[str, float] = {
            tag: 0.0 for tag, _ in sensor_tags
        }

    async def init_db(self) -> None:
        if self._db is not None:
            return
        try:
            self._db = await aiosqlite.connect(self._db_path)
            await self._db.execute("""
                CREATE TABLE IF NOT EXISTS sensor_data (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    timestamp TEXT NOT NULL,
                    tag_name TEXT NOT NULL,
                    value REAL,
                    UNIQUE(timestamp, tag_name)
                )
            """)
            await self._db.execute(
                "CREATE INDEX IF NOT EXISTS idx_ts ON sensor_data(timestamp)"
            )
            await self._db.execute(
                "CREATE INDEX IF NOT EXISTS idx_tag ON sensor_data(tag_name)"
            )
            await self._db.commit()
            LOGGER.info("SQLite DB ready — %s", self._db_path)
        except Exception as e:
            LOGGER.error("SQLite init failed: %s", e)
            self._db = None

    def _sensor_value_changed(self, tag: str, current: float) -> bool:
        last = self._last_written_values.get(tag, 0.0)
        abs_diff = abs(current - last)
        rel_thresh = max(abs(last), 1.0) * SENSOR_CHANGE_REL
        return abs_diff > max(rel_thresh, SENSOR_CHANGE_ABS)

    async def maybe_write_sensors(self, sensor_values: dict[str, float]) -> None:
        if self._db is None:
            await self.init_db()
            if self._db is None:
                return

        now = time.monotonic()
        time_since_last = now - self._last_db_write

        if self._last_db_write == 0.0:
            should_write = True
        else:
            any_changed = any(
                self._sensor_value_changed(tag, sensor_values.get(tag, 0.0))
                for tag, _ in self._sensor_tags
            )
            should_write = any_changed or time_since_last >= SQLITE_FLUSH_SEC

        if not should_write:
            return

        ts = time.strftime("%Y-%m-%d %H:%M:%S", time.localtime())
        rows = [
            (ts, tag, sensor_values.get(tag, 0.0))
            for tag, _ in self._sensor_tags
        ]

        try:
            await self._db.executemany(
                "INSERT OR IGNORE INTO sensor_data (timestamp, tag_name, value) VALUES (?, ?, ?)",
                rows,
            )
            await self._db.commit()
            self._last_db_write = now
            for tag, _ in self._sensor_tags:
                self._last_written_values[tag] = sensor_values.get(tag, 0.0)
        except Exception as e:
            LOGGER.warning("SQLite write failed: %s", e)

    def _flush_influx(self, points: list) -> bool:
        if self._influx_write_api is None:
            return False
        try:
            self._influx_write_api.write(
                bucket=self._influx_bucket, org=self._influx_org, record=points)
            return True
        except Exception as e:
            LOGGER.warning("InfluxDB write fail: %s", e)
            return False

    async def maybe_write_influx(
        self,
        sensor_values: dict[str, float],
        pump_values: dict[str, float],
        valve_sp: float,
        setpoint: float,
        last_error: float,
    ) -> None:
        if self._influx_write_api is None:
            return

        points = []
        for tag, pv_name in self._sensor_tags:
            val = sensor_values.get(tag, 0)
            if val != val:
                continue
            grp = pv_name.split(":")[0].lower()
            meas = {
                "temp": "temperature",
                "press": "pressure",
                "vac": "vacuum",
            }.get(grp, "unknown")
            points.append(
                Point(meas)
                .tag("tag", pv_name.split(":")[-1])
                .tag("location", "lab")
                .field("value", float(val))
            )

        points.append(
            Point("control")
            .tag("tag", "ValveSP")
            .tag("location", "lab")
            .field("value", float(valve_sp))
        )
        points.append(
            Point("control")
            .tag("tag", "Setpoint")
            .tag("location", "lab")
            .field("value", float(setpoint))
        )
        points.append(
            Point("control")
            .tag("tag", "Error")
            .tag("location", "lab")
            .field("value", float(last_error))
        )

        for opc_tag, meas, ftag in self._pump_tags:
            val = pump_values.get(opc_tag)
            if val is not None and val == val:
                points.append(
                    Point(meas)
                    .tag("tag", ftag)
                    .tag("location", "lab")
                    .field("value", float(val))
                )

        self._pending_influx_points.extend(points)
        self._pending_influx_batches += 1

        now = time.monotonic()
        if now - self._last_influx_write < INFLUX_WRITE_SEC and self._pending_influx_batches < self._MAX_PENDING_BATCHES:
            return

        to_flush = self._pending_influx_points
        self._pending_influx_points = []
        self._pending_influx_batches = 0

        loop = asyncio.get_event_loop()
        try:
            ok = await loop.run_in_executor(None, self._flush_influx, to_flush)
            if ok:
                self._last_influx_write = now
        except Exception as e:
            LOGGER.warning("InfluxDB write failed: %s", e)

    async def close(self) -> None:
        if self._db is not None:
            try:
                await self._db.close()
                LOGGER.info("SQLite DB closed")
            except Exception:
                pass
