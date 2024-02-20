# Telegramarr

Telegramarr is an automated Telegram bot designed to work with Radarr. It listens for Radarr webhooks and sends a movie file to a specified Telegram group or chat when a movie is successfully downloaded and added to the Radarr library.

## Features
- Sends a media file to a specified Telegram group or chat when a movie is successfully downloaded and added to the Radarr library. 
- Splits the file into multiple 7z archives and sends them to Telegram to bypass the 2GB file size limit.
- Coming Soon: Sonarr Support

## Installation

Follow these steps to install and configure Telegramarr:

1. Create a new folder, for example, `telegramarr-config`, in your desired location.
2. Create a new bot using BotFather and obtain the bot token. Add the bot to your group or chat.
3. Create a new file `env.py` in the `telegramarr-config` folder and copy the content from [here](https://raw.githubusercontent.com/akhil-rana/telegramarr/main/config/example.env.py) to that file. Replace the sample values with your actual values.
4. To run Telegramarr, use the following Docker command:

    ```bash
    docker run --name=telegramarr \
     -p 8000:8000 \
     -v "<path-to-your-telegramarr-config-folder>":/app/src/config \
     -v "<path-to-your-movies-folder>":/app/movies:ro \
     -d akhilrana/telegramarr:latest 
    ```
Note: Use `akhilrana/telegramarr:arm64` for ARM64 devices.

5. Verify that Telegramarr is running by navigating to `http://<your-ip>:8000/` in your web browser. You should see a message that says "Hello: Telegramarr". 
6. In your Radarr settings, navigate to `Connect > Add New > Webhook`. Add the URL `http://<your-ip>:8000/get-from-radarr` and select only the "On Import" and "On Upgrade" events.
7. Happy TelegramArring!