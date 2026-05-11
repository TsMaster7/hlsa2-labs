FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

COPY loadgen/ /app/loadgen/

ENV PYTHONUNBUFFERED=1
ENV PYTHONPATH=/app
EXPOSE 9999

# Default command shows the help message; an experiment is started by
# overriding command, e.g.
#   docker compose run --rm loadgen python -m loadgen fast-burn
CMD ["python", "-m", "loadgen", "--list"]
