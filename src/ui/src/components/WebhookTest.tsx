import React, { useState } from 'react'
import './WebhookTest.css'

// Sample Radarr webhook payload with actual test movie file
const AKIRA_RADARR_PAYLOAD = {
  movie: {
    id: 1063,
    title: 'Crime 101',
    year: 2026,
    releaseDate: '2026-05-12',
    folderPath: '/movies/Crime 101 (2026)',
    tmdbId: 1171145,
    imdbId: 'tt32430579',
    overview: 'When an elusive thief whose high-stakes heists unfold along the iconic 101 freeway in Los Angeles eyes the score of a lifetime, with hopes of this being his final job, his path collides with a disillusioned insurance broker who is facing her own crossroads. Determined to crack the case, a relentless detective closes in on the operation, raising the stakes even higher.',
  },
  remoteMovie: {
    tmdbId: 1171145,
    imdbId: 'tt32430579',
    title: 'Crime 101',
    year: 2026,
  },
  movieFile: {
    id: 1654,
    relativePath: 'Crime 101 2026 1080p WEB Line HEVC x265 BONE.mkv',
    path: '/movies/Crime 101 (2026)/Crime 101 2026 1080p WEB Line HEVC x265 BONE.mkv',
    quality: 'WEB-1080p',
    qualityVersion: 1,
    releaseGroup: 'BONE',
    sceneName: 'Crime 101 2026 1080p WEB Line HEVC x265 BONE',
    indexerFlags: 'G_Freeleech',
    size: 2147483648,
    dateAdded: '2026-04-06T14:19:12.6819039Z',
    mediaInfo: {
      audioChannels: 2,
      audioCodec: 'AAC',
      audioLanguages: ['und'],
      height: 1080,
      width: 1920,
      subtitles: [],
      videoCodec: 'h265',
      videoDynamicRange: '',
      videoDynamicRangeType: '',
    },
  },
  isUpgrade: false,
  downloadClient: 'qbittorrent',
  downloadClientType: 'qBittorrent',
  downloadId: 'B40201C79C23A7C27A568D5B3BD2E260E4A92951',
  deletedFiles: null,
  customFormatInfo: {
    customFormats: [
      {
        id: 4,
        name: 'x264',
      },
    ],
    customFormatScore: -1,
  },
  release: {
    releaseTitle: 'Crime 101 (2026) [720p] [WEBRip]',
    indexer: 'LimeTorrents (Prowlarr)',
    size: 1374389504,
  },
  eventType: 'Download',
  instanceName: 'Radarr',
  applicationUrl: 'https://radarr.akhilrana.com',
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
        <h3>Crime 101 Test Payload Details</h3>
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
