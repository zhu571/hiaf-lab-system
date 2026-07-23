FROM python:3.11-slim

RUN pip install --no-cache-dir pyepics==3.5.10

COPY epics-gateway.py .

EXPOSE 5070

CMD ["python", "epics-gateway.py"]
