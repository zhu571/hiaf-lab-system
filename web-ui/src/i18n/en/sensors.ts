export default {
  sensors: {
    // Sensor data page (SensorsView).
    title: 'Sensor Data',
    subtitle: 'InfluxDB real-time readings and history trends',
    autoRefresh: 'Auto refresh',
    refresh: 'Refresh',
    latest: 'Latest Readings',
    selectMeasurements: 'Select measurements',
    measurementLabel: 'Measurement',
    countInfo: '{n} items',
    lastUpdated: 'Last updated {time}',
    stale: 'Stale',
    unknown: 'Unknown measurement',
    retry: 'Retry',
    noReadings: 'No readings',
    history: 'History',
    historyHint: 'auto refresh every 30s',
    noDataInRange: 'No data in the selected range',
    latestFailed: 'Failed to load latest readings',
    historyFailed: 'Failed to load history',
    // InfluxDB measurement selector options.
    measurement: {
      pressure: 'Pressure',
      vacuum: 'Vacuum',
      control: 'Control',
      temperature: 'Temperature',
      pump: 'Pump'
    },
    // History time-range options.
    range: {
      '1h': 'Last 1 hour',
      '6h': 'Last 6 hours',
      '24h': 'Last 24 hours',
      '7d': 'Last 7 days'
    },
    // Trend chart toolbar (SensorTrendChart).
    chart: {
      zoomHint: 'Ctrl+scroll to zoom · drag to pan · double-click to reset · Shift+click a legend item to show only it',
      customRange: 'Custom',
      yAuto: 'Auto',
      yMin: 'Min',
      yMax: 'Max',
      reset: 'Reset',
      showAll: 'Show all'
    }
  },
}
