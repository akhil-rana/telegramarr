import asyncio
from fastapi import FastAPI, HTTPException, Request
from utils.validations import validateSchema
import json
import shlex

from config.env import (
    RADARR_MOVIE_FOLDER_PATH,
    SONARR_TVSHOWS_FOLDER_PATH,
    TELEGRAM_API_ID,
    TELEGRAM_API_HASH,
    TELEGRAM_BOT_TOKEN,
    TELEGRAM_RADARR_CHAT_ID,
    TELEGRAM_SONARR_CHAT_ID,
    TELEGRAMARR_DELAY_TIME,
    TELEGRAMARR_FILE_CAPTION_CONTENT,
)

app = FastAPI()
queue = asyncio.Queue()
lock = asyncio.Lock()

@app.get("/")
def read_root():
    return {"Hello": "Telegramarr"}

async def process_webhook(body, chat_id, type):
    if type == "radarr":
        if not validateSchema(data=body, type=type):
            return
        movie_folder_path = body["movie"]["folderPath"]
        file_name = body["movieFile"]["relativePath"]
        file_path = f"{RADARR_MOVIE_FOLDER_PATH}{movie_folder_path.split('/')[-1]}/{file_name}"
    elif type == "sonarr":
        if not validateSchema(data=body, type=type):
            return
        tvshow_folder_path = body["series"]["path"]
        file_name = body["episodeFile"]["relativePath"].split("/")[-1]
        file_path = (
            f"{SONARR_TVSHOWS_FOLDER_PATH}{tvshow_folder_path.split('/')[-1]}/"
            + body["episodeFile"]["relativePath"]
        )
    else:
        return

    command = [
        "python3",
        "./src/scripts/telegram.py",
        f"--telegram_bot_token={shlex.quote(TELEGRAM_BOT_TOKEN)}",
        f"--telegram_api_hash={shlex.quote(TELEGRAM_API_HASH)}",
        f"--telegram_api_id={shlex.quote(TELEGRAM_API_ID)}",
        f"--telegram_chat_id={chat_id}",
        f"--file_name={shlex.quote(file_name)}",
        f"--file_path={shlex.quote(file_path)}",
        f"--file_caption_type={shlex.quote(TELEGRAMARR_FILE_CAPTION_CONTENT)}",
        f"--delay_time={TELEGRAMARR_DELAY_TIME}",
    ]

    # Run the command in the background
    process = await asyncio.create_subprocess_shell(" ".join(command))
    await process.wait()

async def process_queue():
    while True:
        if not queue.empty():
            async with lock:
                body, chat_id, type = await queue.get()
                await process_webhook(body, chat_id, type)
        await asyncio.sleep(1)

@app.post("/get-from-radarr")
async def get_from_radarr(request: Request):
    try:
        body = json.loads((await request.body()).decode())
        await queue.put((body, TELEGRAM_RADARR_CHAT_ID, "radarr"))
        return {"message": "Radarr webhook processed successfully."}
    except Exception as e:
        raise HTTPException(
            status_code=500, detail=f"Error processing Radarr webhook: {str(e)}"
        )

@app.post("/get-from-sonarr")
async def get_from_sonarr(request: Request):
    try:
        body = json.loads((await request.body()).decode())
        await queue.put((body, TELEGRAM_SONARR_CHAT_ID, "sonarr"))
        return {"message": "Sonarr webhook processed successfully."}
    except Exception as e:
        raise HTTPException(
            status_code=500, detail=f"Error processing Sonarr webhook: {str(e)}"
        )

@app.on_event("startup")
async def startup_event():
    asyncio.create_task(process_queue())
