# Stage 1: Build Stage
FROM python:3.12.2-slim-bookworm as builder
WORKDIR /app
COPY . /app
RUN python -m venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"
RUN apt update && \
    apt install p7zip-full cargo -y && \
    pip3 install --upgrade pip && \
    pip3 install --no-cache-dir -r requirements.txt

# Stage 2: Production Stage
FROM python:3.12.2-slim-bookworm
WORKDIR /app
COPY --from=builder /app /app
COPY --from=builder /opt/venv /opt/venv
RUN apt update && \
    apt install p7zip-full -y
ENV PATH="/opt/venv/bin:$PATH"
EXPOSE 8000
CMD ["uvicorn", "main:app", "--app-dir", "src",  "--host", "0.0.0.0", "--port", "8000", "--reload"]
