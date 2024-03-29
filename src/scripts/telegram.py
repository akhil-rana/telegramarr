from datetime import datetime
import os
import subprocess
import shutil
from telethon import TelegramClient
from telethon.tl.types import PeerChat
import argparse
import time

parser = argparse.ArgumentParser(
    description="Script to upload files to telegram and more"
)
parser.add_argument(
    "--telegram_bot_token", type=str, help="Bot token for Telegram", required=True
)
parser.add_argument(
    "--telegram_api_hash", type=str, help="API Hash for Telegram", required=True
)
parser.add_argument(
    "--telegram_api_id", type=str, help="API ID for Telegram", required=True
)
parser.add_argument(
    "--telegram_chat_id",
    type=int,
    help="Telegram Chat ID for the chat/group in which bot is added",
    required=True,
)
parser.add_argument(
    "--file_name", type=str, help="File Name for the file to be uploaded", required=True
)
parser.add_argument(
    "--file_path", type=str, help="File Path for the file to be uploaded", required=True
)
parser.add_argument(
    "--file_caption_type",
    type=str,
    help='Type of caption of the file to be sent. Can be either "fileName/filePath"',
    required=True,
)
parser.add_argument(
    "--delay_time",
    type=int,
    help="Delay in starting the script in seconds",
    required=True,
)

args = parser.parse_args()
TELEGRAM_BOT_TOKEN = args.telegram_bot_token
TELEGRAM_API_HASH = args.telegram_api_hash
TELEGRAM_API_ID = args.telegram_api_id
TELEGRAM_CHAT_ID = args.telegram_chat_id
FILE_NAME = args.file_name
FILE_PATH = args.file_path
FILE_CAPTION_TYPE = args.file_caption_type
DELAY_TIME = args.delay_time
maxFileSize = 1024 * 1024 * 2048  # in Bytes
tempFolder = "./temp"

callBackTime = datetime.now()
entity = None
bot = TelegramClient("bot", TELEGRAM_API_ID, TELEGRAM_API_HASH).start(
    bot_token=TELEGRAM_BOT_TOKEN
)


async def splitFileIntoRar(fileName, filePath):
    if not os.path.exists(tempFolder):
        os.makedirs(tempFolder)
    if "." in fileName:
        fileNameWithoutExtension = fileName.rsplit(".", 1)[0]
    subprocess.run(
        [
            "7z",
            "a",
            "-mx0",
            "-v2000m",
            f"{tempFolder}/{fileNameWithoutExtension}.7z",
            filePath,
        ]
    )
    return os.listdir(tempFolder)


async def uploadFileToTelegram(fileName=FILE_NAME, filePath=FILE_PATH):
    global entity
    entity = await bot.get_entity(PeerChat(TELEGRAM_CHAT_ID))
    uploadProgressCallback.previous_bytes_uploaded = 0
    global currentFileName, statusMessage
    currentFileName = fileName
    if FILE_CAPTION_TYPE == "fileName":
        fileCaption = f"`{currentFileName}`"
    elif FILE_CAPTION_TYPE == "filePath":
        caption = FILE_PATH.rsplit("/", 1)[0] + "/" + currentFileName
        fileCaption = f"`{caption}`"
    statusMessage = await bot.send_message(
        entity,
        f"**`{currentFileName}`** \n\n\n**Uploaded:**   0MB \n\n**Upload Speed:**   0MB/s \n\n**Progress:**   0%",
    )
    await bot.send_file(
        entity,
        filePath,
        force_document=True,
        caption=fileCaption,
        progress_callback=uploadProgressCallback,
    )
    await statusMessage.delete()


async def uploadProgressCallback(current, total):
    global callBackTime, statusMessage, currentFileName
    # only run every 5 seconds instead of too many times

    if (datetime.now() - callBackTime).total_seconds() > 5:
        print(
            "Uploaded: ", current, " / ", total, "bytes: {:.2%}".format(current / total)
        )
        time_difference = (datetime.now() - callBackTime).total_seconds()
        bytes_uploaded = current - uploadProgressCallback.previous_bytes_uploaded
        uploadProgressCallback.previous_bytes_uploaded = current
        if time_difference > 0:
            upload_speed = bytes_uploaded / time_difference
        else:
            upload_speed = 0
        status_message_text = f"**`{currentFileName}`**\n\n**Uploaded:**   {round((current / (1024 * 1024)), 2)} / {round((total / (1024 * 1024)), 2)}MB\n\n**Upload Speed:**   {round(upload_speed / (1024 * 1024), 2)} MB/s\n\n**Progress:**   {round((current / total) * 100, 2)}%"
        statusMessage = await bot.edit_message(
            entity, statusMessage, status_message_text
        )
        callBackTime = datetime.now()


def deleteFolderContents(folderPath):
    if os.listdir(folderPath):  # check if folder is not empty
        for file_name in os.listdir(folderPath):
            file_path = os.path.join(folderPath, file_name)
            if os.path.isfile(file_path) or os.path.islink(file_path):
                os.unlink(file_path)  # remove the file
            elif os.path.isdir(file_path):
                shutil.rmtree(file_path)  # remove dir
        print(f"Folder contents deleted: {folderPath}")


async def main():
    deleteFolderContents(tempFolder)
    print("waiting for {}sec.".format(DELAY_TIME))
    time.sleep(
        DELAY_TIME
    )  # delay so that the file system reloads with the new file. specially in case of rclone mounts
    if not os.path.exists(FILE_PATH):
        print("File does not exist.")
    if os.path.getsize(FILE_PATH) <= maxFileSize:
        await uploadFileToTelegram()
    else:
        try:
            splitFiles = await splitFileIntoRar(FILE_NAME, FILE_PATH)
            for currentFileName in splitFiles:
                await uploadFileToTelegram(
                    currentFileName, tempFolder + "/" + currentFileName
                )
            deleteFolderContents(tempFolder)
        except Exception:
            print("error: {}".format(Exception))
            deleteFolderContents(tempFolder)


with bot:
    bot.loop.run_until_complete(main())
