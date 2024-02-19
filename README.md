# Telegramarr

Telegramarr is an automated Telegram bot designed to work with Radarr. It listens for Radarr webhooks and sends a movie file to a specified Telegram group or chat when a movie is successfully downloaded and added to the Radarr library.

## Installation

Follow these steps to install and configure Telegramarr:

1. Create a new folder, for example, `telegramarr-config`, in your desired location.
2. Create a new bot using BotFather and obtain the bot token. Add the bot to your group or chat.
3. Create a new file `env.py` in the `telegramarr-config` folder and add copy the content from [here](https://raw.githubusercontent.com/akhil-rana/telegramarr/main/config/example.env.py) to that file. Replace the sample values with your actual values.
4. In your Radarr settings, navigate to `Connect > Add New > Webhook`. Add the URL `http://<your-ip>:8000/get-from-radarr` and select only the "On Import" and "On Upgrade" events.
5. Happy TelegramArring!

To run Telegramarr, use the following Docker command:

```bash
docker run --name=telegramarr \
 -p 8000:8000 \
 -v "<path-to-your-telegramarr-config-folder>:/app/config" \
 -v "<path-to-your-movies-folder>:/app/movies":ro \
 -d akhilrana/telegramarr:latest