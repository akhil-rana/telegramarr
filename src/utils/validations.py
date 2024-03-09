from jsonschema import validate, ValidationError

# Define your JSON schema

radarrSchema = {
    "type": "object",
    "properties": {
        "movie": {
            "type": "object",
            "properties": {
                "id": {"type": "integer"},
                "title": {"type": "string"},
                "year": {"type": "integer"},
                "releaseDate": {"type": "string"},
                "folderPath": {"type": "string"},
                "tmdbId": {"type": "integer"},
                "imdbId": {"type": "string"},
                "overview": {"type": "string"},
            },
        },
        "remoteMovie": {
            "type": "object",
            "properties": {
                "tmdbId": {"type": "integer"},
                "imdbId": {"type": "string"},
                "title": {"type": "string"},
                "year": {"type": "integer"},
            },
        },
        "movieFile": {
            "type": "object",
            "properties": {
                "id": {"type": "integer"},
                "relativePath": {"type": "string"},
                "path": {"type": "string"},
                "quality": {"type": "string"},
                "qualityVersion": {"type": "integer"},
                "releaseGroup": {"type": "string"},
                "sceneName": {"type": "string"},
                "indexerFlags": {"type": "string"},
                "size": {"type": "integer"},
                "dateAdded": {"type": "string"},
                "mediaInfo": {
                    "type": "object",
                    "properties": {
                        "audioChannels": {"type": "number"},
                        "audioCodec": {"type": "string"},
                        "audioLanguages": {
                            "type": "array",
                            "items": {"type": "string"},
                        },
                        "height": {"type": "integer"},
                        "width": {"type": "integer"},
                        "subtitles": {"type": "array", "items": {"type": "string"}},
                        "videoCodec": {"type": "string"},
                        "videoDynamicRange": {"type": "string"},
                        "videoDynamicRangeType": {"type": "string"},
                    },
                },
            },
        },
        "isUpgrade": {"type": "boolean"},
        "downloadClient": {"type": "string"},
        "downloadClientType": {"type": "string"},
        "downloadId": {"type": "string"},
        "deletedFiles": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "id": {"type": "integer"},
                    "relativePath": {"type": "string"},
                    "path": {"type": "string"},
                    "quality": {"type": "string"},
                    "qualityVersion": {"type": "integer"},
                    "releaseGroup": {"type": "string"},
                    "indexerFlags": {"type": "string"},
                    "size": {"type": "integer"},
                    "dateAdded": {"type": "string"},
                },
            },
        },
        "customFormatInfo": {
            "type": "object",
            "properties": {
                "customFormats": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "properties": {
                            "id": {"type": "integer"},
                            "name": {"type": "string"},
                        },
                    },
                },
                "customFormatScore": {"type": "integer"},
            },
        },
        "release": {
            "type": "object",
            "properties": {
                "releaseTitle": {"type": "string"},
                "indexer": {"type": "string"},
                "size": {"type": "integer"},
            },
        },
        "eventType": {"type": "string"},
        "instanceName": {"type": "string"},
        "applicationUrl": {"type": "string"},
    },
}

sonarrSchema = {
    "type": "object",
    "properties": {
        "series": {
            "type": "object",
            "properties": {
                "id": {"type": "integer"},
                "title": {"type": "string"},
                "titleSlug": {"type": "string"},
                "path": {"type": "string"},
                "tvdbId": {"type": "integer"},
                "tvMazeId": {"type": "integer"},
                "imdbId": {"type": "string"},
                "type": {"type": "string"},
                "year": {"type": "integer"},
            },
        },
        "episodes": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "id": {"type": "integer"},
                    "episodeNumber": {"type": "integer"},
                    "seasonNumber": {"type": "integer"},
                    "title": {"type": "string"},
                    "overview": {"type": "string"},
                    "airDate": {"type": "string"},
                    "airDateUtc": {"type": "string"},
                    "seriesId": {"type": "integer"},
                    "tvdbId": {"type": "integer"},
                },
            },
        },
        "episodeFile": {
            "type": "object",
            "properties": {
                "id": {"type": "integer"},
                "relativePath": {"type": "string"},
                "path": {"type": "string"},
                "quality": {"type": "string"},
                "qualityVersion": {"type": "integer"},
                "releaseGroup": {"type": "string"},
                "sceneName": {"type": "string"},
                "size": {"type": "integer"},
                "dateAdded": {"type": "string"},
                "mediaInfo": {
                    "type": "object",
                    "properties": {
                        "audioChannels": {"type": "number"},
                        "audioCodec": {"type": "string"},
                        "audioLanguages": {
                            "type": "array",
                            "items": {"type": "string"},
                        },
                        "height": {"type": "integer"},
                        "width": {"type": "integer"},
                        "subtitles": {"type": "array", "items": {"type": "string"}},
                        "videoCodec": {"type": "string"},
                        "videoDynamicRange": {"type": "string"},
                        "videoDynamicRangeType": {"type": "string"},
                    },
                },
            },
        },
        "isUpgrade": {"type": "boolean"},
        "downloadClient": {"type": "string"},
        "downloadClientType": {"type": "string"},
        "downloadId": {"type": "string"},
        "deletedFiles": {
            "type": "array",
            "items": {
                "type": "object",
                "properties": {
                    "id": {"type": "integer"},
                    "relativePath": {"type": "string"},
                    "path": {"type": "string"},
                    "quality": {"type": "string"},
                    "qualityVersion": {"type": "integer"},
                    "releaseGroup": {"type": "string"},
                    "sceneName": {"type": "string"},
                    "size": {"type": "integer"},
                    "dateAdded": {"type": "string"},
                    "mediaInfo": {
                        "type": "object",
                        "properties": {
                            "audioChannels": {"type": "number"},
                            "audioCodec": {"type": "string"},
                            "audioLanguages": {
                                "type": "array",
                                "items": {"type": "string"},
                            },
                            "height": {"type": "integer"},
                            "width": {"type": "integer"},
                            "subtitles": {"type": "array", "items": {"type": "string"}},
                            "videoCodec": {"type": "string"},
                            "videoDynamicRange": {"type": "string"},
                            "videoDynamicRangeType": {"type": "string"},
                        },
                    },
                },
            },
        },
        "customFormatInfo": {
            "type": "object",
            "properties": {
                "customFormats": {
                    "type": "array",
                    "items": {
                        "type": "object",
                        "properties": {
                            "id": {"type": "integer"},
                            "name": {"type": "string"},
                        },
                    },
                },
                "customFormatScore": {"type": "integer"},
            },
        },
        "release": {
            "type": "object",
            "properties": {
                "releaseTitle": {"type": "string"},
                "indexer": {"type": "string"},
                "size": {"type": "integer"},
            },
        },
        "eventType": {"type": "string"},
        "instanceName": {"type": "string"},
        "applicationUrl": {"type": "string"},
    },
}


def validateSchema(data, type):
    if type == "radarr":
        dataSchema = radarrSchema
    elif type == "sonarr":
        dataSchema = sonarrSchema
    else:
        return "Invalid type"
    try:
        validate(instance=data, schema=dataSchema)
        return True
    except ValidationError as e:
        for error in e.absolute_path:
            print(f"- At key: {error}")
        return e.absolute_path
