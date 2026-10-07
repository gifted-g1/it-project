import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { monitoringApi } from '../../services/api'
import Button from '../../components/ui/Button'
import Alert from '../../components/ui/Alert'
import './StudentVerify.css'

export default function StudentVerify() {
  const { examId } = useParams()
  const navigate = useNavigate()
  const videoRef = useRef(null)
  const canvasRef = useRef(null)
  const [status, setStatus] = useState('idle') // idle | camera_ready | verifying | success | failed | error
  const [message, setMessage] = useState('')

  useEffect(() => {
    let stream
    navigator.mediaDevices
      ?.getUserMedia({ video: { width: 480, height: 360 } })
      .then((s) => {
        stream = s
        if (videoRef.current) {
          videoRef.current.srcObject = s
          setStatus('camera_ready')
        }
      })
      .catch(() => {
        setStatus('error')
        setMessage('Camera access was denied. Face verification requires camera permission.')
      })

    // Auto-capture once the camera becomes ready so students who
    // enter the exam URL via the LAN IP are immediately prompted to
    // verify their identity without extra clicks.
    const autoCapture = () => {
      if (status === 'camera_ready') captureAndVerify()
    }
    const readyInterval = setInterval(autoCapture, 700)

    return () => {
      stream?.getTracks().forEach((t) => t.stop())
      clearInterval(readyInterval)
    }
  }, [])

  const captureAndVerify = async () => {
    if (!videoRef.current || !canvasRef.current) return
    setStatus('verifying')
    const video = videoRef.current
    const canvas = canvasRef.current
    canvas.width = video.videoWidth
    canvas.height = video.videoHeight
    canvas.getContext('2d').drawImage(video, 0, 0)
    const imageBase64 = canvas.toDataURL('image/jpeg', 0.85)

    try {
      const result = await monitoringApi.verifyFace({ exam_id: examId, image_base64: imageBase64 })
      if (result.matched) {
        setStatus('success')
        setTimeout(() => navigate(`/student/exams/${examId}/write`), 900)
      } else {
        setStatus('failed')
        setMessage('We could not verify your identity against the registered reference. Please try again or contact an administrator.')
      }
    } catch (e) {
      setStatus('error')
      setMessage('Verification service is currently unavailable. Please try again shortly.')
    }
  }

  return (
    <div className="es-verify">
      <div className="es-verify__card">
        <h2>Identity verification</h2>
        <p className="es-verify__hint">Look directly at the camera and capture a clear photo of your face to begin your examination.</p>

        {status === 'error' && <Alert tone="danger">{message}</Alert>}
        {status === 'failed' && <Alert tone="warning">{message}</Alert>}
        {status === 'success' && <Alert tone="success">Identity verified. Loading your examination…</Alert>}

        <div className="es-verify__camera">
          <video ref={videoRef} autoPlay playsInline muted />
        </div>
        <canvas ref={canvasRef} style={{ display: 'none' }} />

        <Button fullWidth onClick={captureAndVerify} disabled={status === 'verifying' || status === 'error'}>
          {status === 'verifying' ? 'Verifying…' : 'Capture & verify'}
        </Button>
      </div>
    </div>
  )
}
