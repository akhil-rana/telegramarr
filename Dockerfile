FROM python:3.11-rc-alpine
WORKDIR /app
COPY . /app
RUN apk update && apk add p7zip

RUN pip3 install --no-cache-dir -r requirements.txt

EXPOSE 8000

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
