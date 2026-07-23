FROM docker.m.daocloud.io/library/python:3.11-slim

RUN pip install -i https://pypi.tuna.tsinghua.edu.cn/simple \
    asyncua caproto pyepics aiosqlite influxdb-client

WORKDIR /app
COPY hiaf_ioc.py .

EXPOSE 5064

CMD ["python", "hiaf_ioc.py"]
