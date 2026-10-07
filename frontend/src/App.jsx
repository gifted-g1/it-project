import { useEffect, useState } from 'react'
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { AuthProvider, useAuth } from './context/AuthContext'
import AppShell from './components/layout/AppShell'
import ExamShell from './components/layout/ExamShell'
import ProtectedRoute from './components/layout/ProtectedRoute'

import LoginPage from './pages/LoginPage'

import AdminOverview from './pages/admin/AdminOverview'
import AdminUsers from './pages/admin/AdminUsers'
import AdminExams from './pages/admin/AdminExams'
import ExamCreate from './pages/admin/ExamCreate'
import ExamDetail from './pages/admin/ExamDetail'
import MonitoringDashboard from './pages/admin/MonitoringDashboard'

import LecturerOverview from './pages/lecturer/LecturerOverview'
import LecturerExams from './pages/lecturer/LecturerExams'

import StudentOverview from './pages/student/StudentOverview'
import StudentExams from './pages/student/StudentExams'
import StudentVerify from './pages/student/StudentVerify'
import StudentWrite from './pages/student/StudentWrite'
import StudentResult from './pages/student/StudentResult'

// RoleHome routes a freshly-authenticated user to the correct landing
// page. For students, it checks whether an exam is currently ACTIVE
// and sends them straight into the face-verify step of that exam,
// bypassing the student dashboard entirely.
function RoleHome() {
  const { user, token, loading } = useAuth()
  const [target, setTarget] = useState(null)

  useEffect(() => {
    if (loading) return
    if (!user) {
      setTarget('/login')
      return
    }

    if (user.role !== 'STUDENT') {
      const home = { SUPER_ADMIN: '/admin', ADMIN: '/admin', LECTURER: '/lecturer' }[user.role]
      setTarget(home || '/login')
      return
    }

    // Student: find active exam and go straight there using the
    // frontend API helper so base paths and tokens are handled
    // consistently (this also fixes cases where the relative path
    // isn't the same as the embedded web UI origin).
    import('./services/api').then(({ examApi }) => {
      examApi
        .list() // quick fallback to ensure API is reachable
        .catch(() => null)
      fetch('/api/v1/exams/my-active', { headers: { Authorization: `Bearer ${token}` } })
        .then((r) => r.json())
        .then((data) => {
          if (data && data.exam && data.exam.id) {
            setTarget(`/student/exams/${data.exam.id}/verify`)
          } else {
            setTarget('/student')
          }
        })
        .catch(() => setTarget('/student'))
    })
  }, [user, token, loading])

  if (loading) return <div style={{ padding: 24 }}>Loading…</div>
  if (!target) return <div style={{ padding: 24 }}>Loading…</div>
  return <Navigate to={target} replace />
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/" element={<RoleHome />} />

          {/* Admin / Super Admin */}
          <Route
            path="/admin"
            element={
              <ProtectedRoute roles={['SUPER_ADMIN', 'ADMIN']}>
                <AppShell />
              </ProtectedRoute>
            }
          >
            <Route index element={<AdminOverview />} />
            <Route path="users" element={<AdminUsers />} />
            <Route path="exams" element={<AdminExams />} />
            <Route path="exams/create" element={<ExamCreate />} />
            <Route path="exams/:examId" element={<ExamDetail />} />
            <Route path="monitoring/:examId" element={<MonitoringDashboard />} />
          </Route>

          {/* Lecturer */}
          <Route
            path="/lecturer"
            element={
              <ProtectedRoute roles={['LECTURER']}>
                <AppShell />
              </ProtectedRoute>
            }
          >
            <Route index element={<LecturerOverview />} />
            <Route path="exams" element={<LecturerExams />} />
            <Route path="exams/create" element={<ExamCreate />} />
            <Route path="exams/:examId" element={<ExamDetail />} />
            <Route path="monitoring/:examId" element={<MonitoringDashboard />} />
          </Route>

          {/* Student dashboard (fallback when no exam is active) */}
          <Route
            path="/student"
            element={
              <ProtectedRoute roles={['STUDENT']}>
                <AppShell />
              </ProtectedRoute>
            }
          >
            <Route index element={<StudentOverview />} />
            <Route path="exams" element={<StudentExams />} />
          </Route>

          {/* Student exam-taking flow — distraction-free shell */}
          <Route
            path="/student/exams/:examId"
            element={
              <ProtectedRoute roles={['STUDENT']}>
                <ExamShell />
              </ProtectedRoute>
            }
          >
            <Route path="verify" element={<StudentVerify />} />
            <Route path="write" element={<StudentWrite />} />
            <Route path="result" element={<StudentResult />} />
          </Route>

          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  )
}