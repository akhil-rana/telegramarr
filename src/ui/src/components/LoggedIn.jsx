import { useState } from 'react'
import './LoggedIn.css'

export default function LoggedIn({ user, onLogout }) {
  const [isLoggingOut, setIsLoggingOut] = useState(false)
  const username = user.username || 'User'
  const firstName = user.first_name || ''
  const lastName = user.last_name || ''
  const displayName = `${firstName} ${lastName}`.trim() || username

  const handleLogoutClick = async () => {
    setIsLoggingOut(true)
    await onLogout()
    setIsLoggingOut(false)
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
            className="btn-logout" 
            onClick={handleLogoutClick}
            disabled={isLoggingOut}
          >
            {isLoggingOut ? (
              <>
                <span className="logout-spinner"></span>
                Logging out...
              </>
            ) : (
              'Logout'
            )}
          </button>

          <p className="footer-text">Telegramarr is ready for file uploads</p>
        </div>
      </div>
    </div>
  )
}


