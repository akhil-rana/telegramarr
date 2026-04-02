import { useState } from 'react'
import './LoggedIn.css'

export default function LoggedIn({ user, onLogout }) {
  const username = user.username || 'User'
  const firstName = user.first_name || ''
  const lastName = user.last_name || ''
  const displayName = `${firstName} ${lastName}`.trim() || username
  
  const [testLoading, setTestLoading] = useState(false)

  const handleTestUpload = async () => {
    setTestLoading(true)
    try {
      const response = await fetch('/api/webhooks/test-upload', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
      })
      
      if (!response.ok) {
        throw new Error('Test upload failed')
      }
      
      alert('Test upload started! Check your Telegram channel for the file.')
    } catch (error) {
      alert('Error: ' + error.message)
    } finally {
      setTestLoading(false)
    }
  }

  return (
    <div className="app logged-in">
      <div className="logged-in-container">
        <div className="logged-in-card">
          <div className="user-icon">👤</div>
          
          <h1 className="logged-in-title">Logged In</h1>
          
          <div className="user-info">
            <p className="info-label">You are logged in as:</p>
            <p className="username">@{username}</p>
            {displayName !== username && (
              <p className="display-name">{displayName}</p>
            )}
          </div>

          <button 
            className="btn-test-upload" 
            onClick={handleTestUpload}
            disabled={testLoading}
          >
            {testLoading ? 'Starting...' : 'Test Upload'}
          </button>

          <button className="btn-logout" onClick={onLogout} disabled={testLoading}>
            Logout
          </button>

          <p className="footer-text">Telegramarr is ready for file uploads</p>
        </div>
      </div>
    </div>
  )
}


