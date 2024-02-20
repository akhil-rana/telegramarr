# Constants and env variables
RADARR_MOVIE_FOLDER_PATH = "./movies/"  # with trailing slash /
TELEGRAM_BOT_TOKEN = "your_telegram_bot_token_in_string"
TELEGRAM_API_ID = "your_telegram_api_id_in_string"
TELEGRAM_API_HASH = "your_telegram_api_hash_in_string"
TELEGRAM_RADARR_CHAT_ID = 123456789  # chatid of the group where the bot has been added
TELEGRAMARR_DELAY_TIME = 30 #set delay between every movie import. So that file system can reload properly form cache specially in case of rclone mounts
TELEGRAMARR_FILE_CAPTION_CONTENT = "fileName" # accepts 2 possible values: fileName, filePath