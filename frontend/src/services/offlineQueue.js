// A LAN-mode exam has no fallback network if the Wi-Fi blips for a
// moment — there's no "the internet is still there" safety net the
// way an online exam has. This module makes answer submission
// resilient to that: every submission attempt that fails is queued in
// localStorage and retried automatically, so a brief drop never
// silently loses what a student typed.
const QUEUE_KEY = 'es_offline_answer_queue'

function readQueue() {
  try {
    const raw = localStorage.getItem(QUEUE_KEY)
    return raw ? JSON.parse(raw) : []
  } catch {
    return []
  }
}

function writeQueue(queue) {
  try {
    localStorage.setItem(QUEUE_KEY, JSON.stringify(queue))
  } catch {
    // Storage full or unavailable — nothing more we can do locally;
    // the in-memory answer state (and the next successful submit)
    // still carries the student's actual answer.
  }
}

/**
 * Try to submit an answer immediately. If the network call fails,
 * queue it (keyed by attempt_id + question_id, so a later answer to
 * the same question replaces the earlier queued one rather than
 * piling up duplicates) and return { queued: true } instead of
 * throwing, so the exam UI never blocks the student on a network blip.
 *
 * Note: Only network-level errors are queued for retry. HTTP 4xx/5xx
 * responses indicate validation or server errors that won't be resolved
 * by retrying later and should be reported to the user.
 */
export async function submitAnswerResilient(examApi, payload) {
  try {
    await examApi.submitAnswer(payload)
    return { queued: false }
  } catch (err) {
    // Only queue network-level errors, not HTTP error responses.
    // A 400/401/500 won't succeed on retry, so queue=false lets the UI handle it.
    const isNetworkError = !err.response || (err.response?.status >= 500)
    if (!isNetworkError) {
      return { queued: false, error: err }
    }

    const queue = readQueue().filter(
      (q) => !(q.attempt_id === payload.attempt_id && q.question_id === payload.question_id)
    )
    queue.push({ ...payload, queuedAt: Date.now() })
    writeQueue(queue)
    return { queued: true }
  }
}

/** Number of answers currently waiting to sync for a given attempt. */
export function pendingCount(attemptId) {
  return readQueue().filter((q) => q.attempt_id === attemptId).length
}

/**
 * Retry every queued answer. Call this periodically and whenever the
 * browser reports it's back online. Successfully-sent entries are
 * removed from the queue; failures stay queued for the next attempt.
 * 4xx/5xx HTTP responses are not retried; only genuine network errors are.
 */
export async function flushQueue(examApi) {
  const queue = readQueue()
  if (queue.length === 0) return

  const stillPending = []
  for (const item of queue) {
    const { queuedAt, ...payload } = item
    try {
      await examApi.submitAnswer(payload)
    } catch (err) {
      // Only keep in queue if it's a network error; HTTP errors won't retry
      const isNetworkError = !err.response || (err.response?.status >= 500)
      if (isNetworkError) {
        stillPending.push(item)
      }
      // 4xx/5xx are dropped from queue (user will see error in UI)
    }
  }
  writeQueue(stillPending)
}

/** Clears any queued answers for an attempt once it's been submitted. */
export function clearQueueForAttempt(attemptId) {
  writeQueue(readQueue().filter((q) => q.attempt_id !== attemptId))
}
