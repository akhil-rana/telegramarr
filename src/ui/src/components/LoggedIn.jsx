import { useState } from 'react'
import './LoggedIn.css'
import WebhookTest from './WebhookTest'

export default function LoggedIn({ user, onLogout }) {
  const username = user.username || 'User'
  const firstName = user.first_name || ''
  const lastName = user.last_name || ''
  const displayName = `${firstName} ${lastName}`.trim() || username

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

          <button className="btn-logout" onClick={onLogout}>
            Logout
          </button>

          <p className="footer-text">Telegramarr is ready for file uploads</p>
        </div>

        <div className="webhook-test-container">
          <WebhookTest apiBaseUrl="" />
        </div>
      </div>
    </div>
  )
}


