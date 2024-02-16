from typing import Union
from fastapi import FastAPI, Request
import json

app = FastAPI()


@app.get("/")
def read_root():
    return {"Hello": "Telegramarr"}


@app.post("/get-from-radarr")
async def get_from_radarr(request: Request):
    try:
        body = await request.body()
        body = json.loads(body.decode())
        if body["eventType"] == "Download" and "movie" in body:
            print(body["movie"])
        return {"message": "Webhook received successfully."}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Error processing webhook: {str(e)}")