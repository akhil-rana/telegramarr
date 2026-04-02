import { useState, useEffect } from 'react'
import './App.css'
import Auth from './components/Auth'
import LoggedIn from './components/LoggedIn'

export default function App() {
  const [authenticated, setAuthenticated] = useState(false)
  const [loading, setLoading] = useState(true)
  const [user, setUser] = useState(null)

  useEffect(() => {
    checkAuth()
  }, [])

  const checkAuth = async () => {
    try {
      const response = await fetch('/api/auth/status')
      const data = await response.json()
      setAuthenticated(data.authenticated || false)
      if (data.user) {
        setUser(data.user)
      }
    } catch (error) {
      console.error('Failed to check auth status:', error)
    } finally {
      setLoading(false)
    }
  }

  const handleLogout = async () => {
    try {
      await fetch('/api/auth/logout', { method: 'POST' })
      setAuthenticated(false)
      setUser(null)
      checkAuth()
    } catch (error) {
      console.error('Failed to logout:', error)
    }
  }

  if (loading) {
    return <div className="loading">Loading...</div>
  }

  if (authenticated && user) {
    return <LoggedIn user={user} onLogout={handleLogout} />
  }

  return (
    <div className="app">
      <Auth onAuthSuccess={checkAuth} />
    </div>
  )
}


