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
radarr_queue = asyncio.Queue()
sonarr_queue = asyncio.Queue()


@app.get("/")
def read_root():
    return {"Hello": "Telegramarr"}


async def process_radarr_webhook(body):
    if validateSchema(data=body, type="radarr"):
        movie_folder_path = body["movie"]["folderPath"]
        movie_file_name = body["movieFile"]["relativePath"]
        local_movie_file_path = f"{RADARR_MOVIE_FOLDER_PATH}{movie_folder_path.split('/')[-1]}/{movie_file_name}"
        command = [
            "python3",
            "./src/scripts/telegram.py",
            f"--telegram_bot_token={shlex.quote(TELEGRAM_BOT_TOKEN)}",
            f"--telegram_api_hash={shlex.quote(TELEGRAM_API_HASH)}",
            f"--telegram_api_id={shlex.quote(TELEGRAM_API_ID)}",
            f"--telegram_radarr_chat_id={TELEGRAM_RADARR_CHAT_ID}",
            f"--file_name={shlex.quote(movie_file_name)}",
            f"--file_path={shlex.quote(local_movie_file_path)}",
            f"--file_caption_type={shlex.quote(TELEGRAMARR_FILE_CAPTION_CONTENT)}",
            f"--delay_time={TELEGRAMARR_DELAY_TIME}",
        ]
        # Run the command in the background

        process = await asyncio.create_subprocess_shell(" ".join(command))
        await process.wait()


async def process_sonarr_webhook(body):
    if validateSchema(data=body, type="sonarr"):
        tvshow_folder_path = body["series"]["path"]
        tvshow_file_name = body["episodeFile"]["relativePath"].split("/")[-1]
        local_tvshow_file_path = (
            f"{SONARR_TVSHOWS_FOLDER_PATH}{tvshow_folder_path.split('/')[-1]}/"
            + body["episodeFile"]["relativePath"]
        )
        command = [
            "python3",
            "./src/scripts/telegram.py",
            f"--telegram_bot_token={shlex.quote(TELEGRAM_BOT_TOKEN)}",
            f"--telegram_api_hash={shlex.quote(TELEGRAM_API_HASH)}",
            f"--telegram_api_id={shlex.quote(TELEGRAM_API_ID)}",
            f"--telegram_radarr_chat_id={TELEGRAM_SONARR_CHAT_ID}",
            f"--file_name={shlex.quote(tvshow_file_name)}",
            f"--file_path={shlex.quote(local_tvshow_file_path)}",
            f"--file_caption_type={shlex.quote(TELEGRAMARR_FILE_CAPTION_CONTENT)}",
            f"--delay_time={TELEGRAMARR_DELAY_TIME}",
        ]
        
        # Run the command in the background
        process = await asyncio.create_subprocess_shell(" ".join(command))
        await process.wait()


async def process_radarr_queue():
    while True:
        if not radarr_queue.empty():
            body = await radarr_queue.get()
            await process_radarr_webhook(body)
        await asyncio.sleep(1)


async def process_sonarr_queue():
    while True:
        if not sonarr_queue.empty():
            body = await sonarr_queue.get()
            await process_sonarr_webhook(body)
        await asyncio.sleep(1)


@app.post("/get-from-radarr")
async def get_from_radarr(request: Request):
    try:
        body = json.loads((await request.body()).decode())
        await radarr_queue.put(
            body
        )  # Put the Radarr webhook request body into the queue
        return {"message": "Radarr webhook processed successfully."}
    except Exception as e:
        raise HTTPException(
            status_code=500, detail=f"Error processing Radarr webhook: {str(e)}"
        )


@app.post("/get-from-sonarr")
async def get_from_sonarr(request: Request):
    try:
        body = json.loads((await request.body()).decode())
        await sonarr_queue.put(
            body
        )  # Put the Sonarr webhook request body into the queue
        return {"message": "Sonarr webhook processed successfully."}
    except Exception as e:
        raise HTTPException(
            status_code=500, detail=f"Error processing Sonarr webhook: {str(e)}"
        )


@app.on_event("startup")
async def startup_event():
    asyncio.create_task(process_radarr_queue())
    asyncio.create_task(process_sonarr_queue())
