import React, { useState } from 'react'
import './WebhookTest.css'

// Sample Radarr webhook payload with actual test movie file
const AKIRA_RADARR_PAYLOAD = {
  movie: {
    id: 1,
    title: 'Akira',
    year: 1988,
    releaseDate: '1988-07-16',
    folderPath: 'test/movies',
    tmdbId: 149,
    imdbId: 'tt0094625',
    overview: 'A secret military project endangers Neo-Tokyo when it turns a biker gang member into a rampaging psychic psychopath who can only be stopped by his friends and a teenager with psychic powers.',
  },
  remoteMovie: {
    tmdbId: 149,
    imdbId: 'tt0094625',
    title: 'Akira',
    year: 1988,
  },
  movieFile: {
    id: 1,
    relativePath: 'Akira (1988) [2160p] [4K] [BluRay] [5.1] [YTS.MX] [imdbid-tt0094625].mkv',
    path: 'test/movies/Akira (1988) [2160p] [4K] [BluRay] [5.1] [YTS.MX] [imdbid-tt0094625].mkv',
    quality: '4K',
    qualityVersion: 1,
    releaseGroup: 'YTS.MX',
    sceneName: 'Akira.1988.2160p.4K.BluRay.x265-YTS.MX',
    indexerFlags: '',
    size: 2147483648, // 2GB
    dateAdded: '2024-04-03T00:00:00Z',
    mediaInfo: {
      audioChannels: 5.1,
      audioCodec: 'AAC',
      audioLanguages: ['en'],
      height: 2160,
      width: 3840,
      subtitles: ['en', 'ja'],
      videoCodec: 'h265',
      videoDynamicRange: 'HDR',
      videoDynamicRangeType: '',
    },
  },
  isUpgrade: false,
  downloadClient: 'qBittorrent',
  downloadClientType: 'QBittorrent',
  downloadId: 'test-download-id',
  deletedFiles: [],
  customFormatInfo: {
    customFormats: [],
    customFormatScore: 0,
  },
  release: {
    releaseTitle: 'Akira.1988.2160p.4K.BluRay.x265-YTS.MX',
    indexer: 'YTS.MX',
    size: 2147483648,
  },
  eventType: 'Download',
  instanceName: 'Radarr',
  applicationUrl: 'http://localhost:7878',
}

// Sample Sonarr webhook payload (for reference)
const SAMPLE_SONARR_PAYLOAD = {
  series: {
    id: 1,
    title: 'Test Series',
    titleSlug: 'test-series',
    path: '/tv/Test Series',
    tvdbId: 12345,
    tvMazeId: 54321,
    imdbId: 'tt0000001',
    type: 'standard',
    year: 2020,
  },
  episodes: [
    {
      id: 1,
      episodeNumber: 1,
      seasonNumber: 1,
      title: 'Episode 1',
      overview: 'First episode',
      airDate: '2020-01-01',
      airDateUtc: '2020-01-01T00:00:00Z',
      seriesId: 1,
      tvdbId: 100001,
    },
  ],
  episodeFile: {
    id: 1,
    relativePath: 'Season 01/Episode 01.mkv',
    path: '/tv/Test Series/Season 01/Episode 01.mkv',
    quality: '1080p',
    qualityVersion: 1,
    releaseGroup: 'TEST',
    sceneName: 'Test.Series.S01E01.1080p.HDTV.x264-TEST',
    size: 1073741824,
    dateAdded: '2020-01-01T00:00:00Z',
    mediaInfo: {
      audioChannels: 2,
      audioCodec: 'AAC',
      audioLanguages: ['en'],
      height: 1080,
      width: 1920,
      subtitles: ['en'],
      videoCodec: 'h264',
      videoDynamicRange: '',
      videoDynamicRangeType: '',
    },
  },
  isUpgrade: false,
  downloadClient: 'qBittorrent',
  downloadClientType: 'QBittorrent',
  downloadId: 'test-download-id',
  deletedFiles: [],
  customFormatInfo: {
    customFormats: [],
    customFormatScore: 0,
  },
  release: {
    releaseTitle: 'Test.Series.S01E01.1080p.HDTV.x264-TEST',
    indexer: 'TestIndexer',
    size: 1073741824,
  },
  eventType: 'Download',
  instanceName: 'Sonarr',
  applicationUrl: 'http://localhost:8989',
}

interface WebhookTestProps {
  apiBaseUrl: string
}

export const WebhookTest: React.FC<WebhookTestProps> = ({ apiBaseUrl }) => {
  const [loading, setLoading] = useState(false)
  const [response, setResponse] = useState<any>(null)
  const [error, setError] = useState<string | null>(null)

  const testRadarrWebhook = async () => {
    setLoading(true)
    setError(null)
    setResponse(null)

    try {
      const url = `${apiBaseUrl}/api/webhooks/radarr`
      console.log('Sending Radarr webhook to:', url)
      console.log('Payload:', AKIRA_RADARR_PAYLOAD)
      
      const res = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(AKIRA_RADARR_PAYLOAD),
      })

      console.log('Response status:', res.status)
      
      const data = await res.json()
      console.log('Response data:', data)
      setResponse(data)

      if (!res.ok) {
        setError(`Webhook failed: ${res.status} ${JSON.stringify(data)}`)
      }
    } catch (err) {
      console.error('Fetch error:', err)
      setError(`Failed to send webhook: ${err}`)
    } finally {
      setLoading(false)
    }
  }

  const testSonarrWebhook = async () => {
    setLoading(true)
    setError(null)
    setResponse(null)

    try {
      const url = `${apiBaseUrl}/api/webhooks/sonarr`
      console.log('Sending Sonarr webhook to:', url)
      console.log('Payload:', SAMPLE_SONARR_PAYLOAD)
      
      const res = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(SAMPLE_SONARR_PAYLOAD),
      })

      console.log('Response status:', res.status)
      
      const data = await res.json()
      console.log('Response data:', data)
      setResponse(data)

      if (!res.ok) {
        setError(`Webhook failed: ${res.status} ${JSON.stringify(data)}`)
      }
    } catch (err) {
      console.error('Fetch error:', err)
      setError(`Failed to send webhook: ${err}`)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="webhook-test">
      <h2>Webhook Testing</h2>

      <div className="test-section">
        <h3>Radarr - Akira Movie</h3>
        <p>Tests Radarr webhook with Akira movie (IMDB: tt0094625, TMDB: 149)</p>
        <button onClick={testRadarrWebhook} disabled={loading} className="btn btn-primary">
          {loading ? 'Sending...' : 'Test Radarr Webhook'}
        </button>
      </div>

      <div className="test-section">
        <h3>Sonarr - Sample Episode</h3>
        <p>Tests Sonarr webhook with sample TV series episode</p>
        <button onClick={testSonarrWebhook} disabled={loading} className="btn btn-secondary">
          {loading ? 'Sending...' : 'Test Sonarr Webhook'}
        </button>
      </div>

      {error && (
        <div className="response error">
          <h4>Error</h4>
          <pre>{error}</pre>
        </div>
      )}

      {response && !error && (
        <div className="response success">
          <h4>Response</h4>
          <pre>{JSON.stringify(response, null, 2)}</pre>
        </div>
      )}

      <div className="payload-details">
        <h3>Akira Test Payload Details</h3>
        <ul>
          <li><strong>Title:</strong> {AKIRA_RADARR_PAYLOAD.movie.title}</li>
          <li><strong>Year:</strong> {AKIRA_RADARR_PAYLOAD.movie.year}</li>
          <li><strong>IMDB ID:</strong> {AKIRA_RADARR_PAYLOAD.movie.imdbId}</li>
          <li><strong>TMDB ID:</strong> {AKIRA_RADARR_PAYLOAD.movie.tmdbId}</li>
          <li><strong>File:</strong> {AKIRA_RADARR_PAYLOAD.movieFile.relativePath}</li>
          <li><strong>Quality:</strong> {AKIRA_RADARR_PAYLOAD.movieFile.quality}</li>
          <li><strong>Size:</strong> {(AKIRA_RADARR_PAYLOAD.movieFile.size / 1024 / 1024 / 1024).toFixed(2)} GB</li>
        </ul>
      </div>
    </div>
  )
}

export default WebhookTest
