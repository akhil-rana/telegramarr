FROM python:3.11-slim-bookworm
WORKDIR /app
COPY . /app

RUN apt update && \
    apt upgrade -y && \
    apt install p7zip-full -y
    
RUN pip3 install --upgrade pip
RUN pip3 install --no-cache-dir -r requirements.txt

EXPOSE 8000

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000", "--reload"]
