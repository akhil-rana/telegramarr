import asyncio
from fastapi import FastAPI, HTTPException, Request
from utils.validations import validateRadarrSchema
import json
from env import RADARR_MOVIE_FOLDER_PATH, TELEGRAM_API_ID, TELEGRAM_API_HASH, TELEGRAM_BOT_TOKEN, TELEGRAM_RADARR_CHAT_ID


app = FastAPI()
queue = asyncio.Queue()

@app.get("/")
def read_root():
    return {"Hello": "Telegramarr"}


async def process_radarr_webhook(body):
    if validateRadarrSchema(body):
        movie_folder_path = body["movie"]["folderPath"]
        movie_file_name = body['movieFile']['relativePath']
        local_movie_file_path = f"{RADARR_MOVIE_FOLDER_PATH}{movie_folder_path.split('/')[-1]}/{movie_file_name}"
        command = [
            "python3",
            "./scripts/telegram.py",
            f"--telegram_bot_token='{TELEGRAM_BOT_TOKEN}'",
            f"--telegram_api_hash='{TELEGRAM_API_HASH}'",
            f"--telegram_api_id='{TELEGRAM_API_ID}'",
            f"--telegram_radarr_chat_id={TELEGRAM_RADARR_CHAT_ID}",
            f"--file_name='{movie_file_name}'",
            f"--file_path='{local_movie_file_path}'",
            f"--file_caption='{local_movie_file_path}'",
        ]
        # Run the command in the background
        process = await asyncio.create_subprocess_shell(" ".join(command))
        await process.wait()

async def process_queue():
    while True:
        if not queue.empty():
            body = await queue.get()
            await process_radarr_webhook(body)
        await asyncio.sleep(1)


@app.post("/get-from-radarr")
async def get_from_radarr(request: Request):
    try:
        body = json.loads((await request.body()).decode())
        await queue.put(body)  # Put the webhook request body into the queue
        return {"message": "Webhook processed successfully."}
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Error processing webhook: {str(e)}")

@app.on_event("startup")
async def startup_event():
    asyncio.create_task(process_queue())