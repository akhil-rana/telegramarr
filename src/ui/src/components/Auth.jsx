import { useState, useRef } from 'react'
import './Auth.css'

export default function Auth({ onAuthSuccess }) {
  const [step, setStep] = useState('init') // init, qr, phone, code, 2fa, success
  const [phone, setPhone] = useState('')
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')
  const [phoneCodeHash, setPhoneCodeHash] = useState('')
  const [qrCode, setQrCode] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [message, setMessage] = useState('')
  const wsRef = useRef(null)

  const getBackendUrl = () => {
    // If running on Vite dev server (port 8008), backend is on 8009
    if (window.location.port === '8008') {
      return 'localhost:8009'
    }
    // If running directly from backend (8009), use same host
    if (window.location.port === '8009') {
      return window.location.host
    }
    // For production, use same host
    return window.location.host
  }

  const connectWebSocket = (onMsg) => {
    const wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const backendHost = getBackendUrl()
    const ws = new WebSocket(wsProto + '//' + backendHost + '/api/auth/ws')
    
    console.log('Connecting to WebSocket:', wsProto + '//' + backendHost + '/api/auth/ws')
    
    ws.onopen = () => {
      console.log('WebSocket connected')
    }
    
    ws.onmessage = (event) => {
      try {
        if (!event.data) {
          console.warn('Empty WebSocket message')
          return
        }
        const msg = JSON.parse(event.data)
        console.log('WS message received:', msg)
        onMsg(msg)
      } catch (err) {
        console.error('Failed to parse WebSocket message:', event.data, err)
      }
    }
    
    ws.onerror = (error) => {
      console.error('WebSocket error:', error)
      setError('Connection error. Please try again.')
      setLoading(false)
    }
    
    ws.onclose = () => {
      console.log('WebSocket closed')
    }
    
    wsRef.current = ws
    return ws
  }

  const startQRAuth = async () => {
    setLoading(true)
    setError('')
    
    const ws = connectWebSocket((msg) => {
      console.log('QR message received:', JSON.stringify(msg))
      // Handle QR code response (check for token field for flexibility)
      if ((msg.type === 'auth' || msg.type === 'qr') && msg.payload && msg.payload.token) {
        // QR code image received from backend
        console.log('Setting QR code')
        setQrCode(msg.payload.token)
        setMessage('Scan this QR code with your Telegram app')
      } else if ((msg.type === 'auth' || msg.type === 'qr') && msg.token && typeof msg.token === 'string' && msg.token.startsWith('data:image')) {
        // Handle direct token field (fallback)
        console.log('Setting QR code from direct token')
        setQrCode(msg.token)
        setMessage('Scan this QR code with your Telegram app')
      } else if (msg.type === 'auth' && msg.message === '2FA required') {
        // 2FA is required after QR scan
        console.log('2FA required')
        setStep('2fa')
        setLoading(false)
      } else if (msg.type === 'auth' && msg.message === 'success') {
        // QR auth completed successfully
        console.log('Auth success!')
        setStep('success')
        setTimeout(onAuthSuccess, 1500)
      } else if (msg.type === 'error') {
        console.log('Auth error:', msg.message)
        setError(msg.message)
        setStep('init')
        setLoading(false)
      } else {
        console.log('Unhandled message type:', msg.type, 'Full message:', msg)
      }
    })
    
    // Wait for WebSocket to be open before sending
    const checkConnection = setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) {
        clearInterval(checkConnection)
        console.log('Sending QR auth request')
        ws.send(JSON.stringify({ auth_type: 'qr' }))
        setStep('qr')
        setLoading(false)
      } else if (ws.readyState === WebSocket.CLOSED) {
        clearInterval(checkConnection)
        setError('Failed to connect to backend')
        setLoading(false)
      }
    }, 50)

    // Timeout after 5 seconds
    setTimeout(() => {
      clearInterval(checkConnection)
      if (ws.readyState !== WebSocket.OPEN) {
        setError('Connection timeout. Is the backend running on port 8009?')
        setLoading(false)
      }
    }, 5000)
  }

  const startPhoneAuth = () => {
    setLoading(true)
    setError('')
    
    const ws = connectWebSocket((msg) => {
      console.log('Phone auth message received:', JSON.stringify(msg))
      if (msg.type === 'auth' && msg.payload && msg.payload.phoneCodeHash) {
        setPhoneCodeHash(msg.payload.phoneCodeHash)
        setMessage('Confirmation code sent to your phone')
        setStep('code')
        setLoading(false)
      } else if (msg.type === 'error') {
        setError(msg.message)
        setLoading(false)
      }
    })
    
    // Wait for WebSocket to be open before sending
    const checkConnection = setInterval(() => {
      if (ws.readyState === WebSocket.OPEN) {
        clearInterval(checkConnection)
        setStep('phone')
        setLoading(false)
      } else if (ws.readyState === WebSocket.CLOSED) {
        clearInterval(checkConnection)
        setError('Failed to connect to backend')
        setLoading(false)
      }
    }, 50)

    // Timeout after 5 seconds
    setTimeout(() => {
      clearInterval(checkConnection)
      if (ws.readyState !== WebSocket.OPEN) {
        setError('Connection timeout. Is the backend running on port 8009?')
        setLoading(false)
      }
    }, 5000)
  }

  const submitPhone = (e) => {
    e.preventDefault()
    if (!phone) {
      setError('Please enter phone number')
      return
    }

    setLoading(true)
    setError('')
    
    // Use existing websocket if connected, otherwise create new one
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      connectWebSocket((msg) => {
        if (msg.type === 'auth' && msg.payload && msg.payload.phoneCodeHash) {
          setPhoneCodeHash(msg.payload.phoneCodeHash)
          setMessage('Confirmation code sent to your phone')
          setStep('code')
          setLoading(false)
        } else if (msg.type === 'error') {
          setError(msg.message)
          setLoading(false)
        }
      })
    }

    // Send after short delay to ensure connection
    setTimeout(() => {
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        console.log('Sending phone code request for:', phone)
        wsRef.current.send(JSON.stringify({
          auth_type: 'phone',
          message: 'sendcode',
          phone_no: phone
        }))
      } else {
        setError('Not connected to backend')
        setLoading(false)
      }
    }, 100)
  }

  const submitCode = (e) => {
    e.preventDefault()
    if (!code) {
      setError('Please enter confirmation code')
      return
    }

    setLoading(true)
    setError('')
    
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      connectWebSocket((msg) => {
        handleCodeResponse(msg)
      })
    }

    // Send after short delay to ensure connection
    setTimeout(() => {
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        console.log('Sending code verification')
        wsRef.current.send(JSON.stringify({
          auth_type: 'phone',
          message: 'signin',
          phone_no: phone,
          phone_code: code,
          phone_code_hash: phoneCodeHash
        }))
      } else {
        setError('Not connected to backend')
        setLoading(false)
      }
    }, 100)
  }

  const handleCodeResponse = (msg) => {
    // Check for specific Telegram error messages
    if (msg.type === 'auth' && msg.message === 'PHONE_CODE_INVALID') {
      setError('Invalid code. Please try again.')
      setLoading(false)
    } else if (msg.type === 'auth' && msg.message === '2FA required') {
      // 2FA is required after phone code
      setStep('2fa')
      setLoading(false)
    } else if (msg.type === 'auth' && msg.message === 'success') {
      // Phone auth completed successfully
      setStep('success')
      setTimeout(onAuthSuccess, 1500)
    } else if (msg.type === 'error') {
      setError(msg.message)
      setLoading(false)
    }
  }

  const submit2FA = (e) => {
    e.preventDefault()
    if (!password) {
      setError('Please enter your password')
      return
    }

    setLoading(true)
    setError('')
    
    if (!wsRef.current || wsRef.current.readyState !== WebSocket.OPEN) {
      connectWebSocket((msg) => {
        if (msg.type === 'auth' && msg.message === 'success') {
          setStep('success')
          setTimeout(onAuthSuccess, 1500)
        } else if (msg.type === 'error') {
          setError(msg.message)
          setLoading(false)
        }
      })
    }

    // Send after short delay to ensure connection
    setTimeout(() => {
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        console.log('Sending 2FA password')
        wsRef.current.send(JSON.stringify({
          auth_type: '2fa',
          password: password
        }))
      } else {
        setError('Not connected to backend')
        setLoading(false)
      }
    }, 100)
  }

  return (
    <div className="auth-container">
      <div className="auth-card">
        <h1 className="auth-title">Telegramarr</h1>
        <p className="auth-subtitle">Telegram File Upload Manager</p>

        {error && <div className="error-message">{error}</div>}
        {message && <div className="success-message">{message}</div>}

        {step === 'init' && (
          <div className="auth-step">
            <p className="step-description">Authenticate with Telegram</p>
            <button 
              className="btn btn-primary" 
              onClick={startQRAuth}
              disabled={loading}
            >
              {loading ? 'Starting...' : 'QR Code Login'}
            </button>
            <button 
              className="btn btn-secondary" 
              onClick={startPhoneAuth}
              disabled={loading}
            >
              {loading ? 'Starting...' : 'Phone Number Login'}
            </button>
            <p className="hint">Use your phone number or QR code to log in</p>
          </div>
        )}

        {step === 'qr' && (
          <div className="auth-step">
            <p className="step-description">Scan with Telegram</p>
            {qrCode && (
              <div className="qr-container">
                <img src={qrCode} alt="QR Code" className="qr-code" />
              </div>
            )}
            <p className="hint">Or continue with phone number instead</p>
            <button 
              className="btn btn-secondary" 
              onClick={() => {
                setStep('phone')
                setLoading(false)
              }}
            >
              Use Phone Number
            </button>
          </div>
        )}

        {step === 'phone' && (
          <form onSubmit={submitPhone} className="auth-step">
            <p className="step-description">Enter your phone number</p>
            <input
              type="tel"
              placeholder="+1 234 567 8900"
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              className="input-field"
              disabled={loading}
            />
            <button 
              type="submit" 
              className="btn btn-primary"
              disabled={loading}
            >
              {loading ? 'Sending...' : 'Send Code'}
            </button>
          </form>
        )}

        {step === 'code' && (
          <form onSubmit={submitCode} className="auth-step">
            <p className="step-description">Enter confirmation code</p>
            <p className="hint">Check your phone for the code from Telegram</p>
            <input
              type="text"
              placeholder="12345"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              className="input-field"
              disabled={loading}
              autoFocus
            />
            <button 
              type="submit" 
              className="btn btn-primary"
              disabled={loading}
            >
              {loading ? 'Verifying...' : 'Verify Code'}
            </button>
          </form>
        )}

        {step === '2fa' && (
          <form onSubmit={submit2FA} className="auth-step">
            <p className="step-description">Enter 2FA password</p>
            <p className="hint">Your account has two-factor authentication enabled</p>
            <input
              type="password"
              placeholder="Your 2FA password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              className="input-field"
              disabled={loading}
              autoFocus
            />
            <button 
              type="submit" 
              className="btn btn-primary"
              disabled={loading}
            >
              {loading ? 'Verifying...' : 'Submit'}
            </button>
          </form>
        )}

        {step === 'success' && (
          <div className="auth-step success">
            <div className="success-icon">✓</div>
            <p className="step-description">Authentication Successful!</p>
            <p className="hint">Redirecting...</p>
          </div>
        )}
      </div>
    </div>
  )
}
