import type Artplayer from 'artplayer'

// Existing UI coordinate correction for html zoom: visual clientX / visual rect.
export function attachProgressSeekFix(container: HTMLElement, art: Artplayer): () => void {
  let dragging = false

  const seekToClientX = (clientX: number) => {
    const $progress = container.querySelector<HTMLElement>('.art-control-progress')
    if (!$progress) return
    const rect = $progress.getBoundingClientRect()
    const duration = art.duration
    if (rect.width <= 0 || !Number.isFinite(duration) || duration <= 0) return
    const fraction = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width))
    art.seek = fraction * duration
  }

  const inProgress = (target: EventTarget | null): target is Element =>
    target instanceof Element && target.closest('.art-control-progress') !== null

  const onClickCapture = (event: MouseEvent) => {
    if (!inProgress(event.target)) return
    if ((event.target as Element).closest('.art-progress-indicator')) return
    seekToClientX(event.clientX)
    event.stopPropagation()
  }

  const onMouseDownCapture = (event: MouseEvent) => {
    if (event.button !== 0 || !inProgress(event.target)) return
    dragging = true
    event.stopPropagation()
  }
  const onDocumentMouseMove = (event: MouseEvent) => {
    if (dragging) seekToClientX(event.clientX)
  }
  const onDocumentMouseUp = () => {
    dragging = false
  }

  container.addEventListener('click', onClickCapture, true)
  container.addEventListener('mousedown', onMouseDownCapture, true)
  document.addEventListener('mousemove', onDocumentMouseMove)
  document.addEventListener('mouseup', onDocumentMouseUp)
  return () => {
    container.removeEventListener('click', onClickCapture, true)
    container.removeEventListener('mousedown', onMouseDownCapture, true)
    document.removeEventListener('mousemove', onDocumentMouseMove)
    document.removeEventListener('mouseup', onDocumentMouseUp)
  }
}
