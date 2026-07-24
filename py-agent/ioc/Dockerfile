FROM docker.m.daocloud.io/library/python:3.11-slim

RUN pip install -i https://pypi.tuna.tsinghua.edu.cn/simple \
    caproto asyncua aiosqlite influxdb-client

WORKDIR /app
COPY hiaf_config.py hiaf_storage.py hiaf_ioc_final.py ./

EXPOSE 5064 5080
CMD ["python", "hiaf_ioc_final.py"]
