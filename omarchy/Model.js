// Pure presentation and command rules, shared by QML and hardware-free tests.
function number(value, fallback) {
  var result = Number(value)
  return value !== null && value !== undefined && isFinite(result) ? result : fallback
}

function durationText(seconds) {
  var total = Math.max(0, Math.floor(number(seconds, 0)))
  var hours = Math.floor(total / 3600)
  var minutes = Math.floor((total % 3600) / 60)
  var secs = String(total % 60).padStart(2, "0")
  return hours > 0 ? hours + ":" + String(minutes).padStart(2, "0") + ":" + secs : minutes + ":" + secs
}

function parseSnapshot(text) {
  var value = JSON.parse(text)
  if (!value || !value.status || typeof value.status !== "object"
      || ["disconnected", "scanning", "connecting", "connected", "ready"].indexOf(value.connectionState) < 0
      || typeof value.status.speed !== "number" || !isFinite(value.status.speed)) {
    throw new Error("Invalid WalkingPad response")
  }
  return value
}

function presentation(snapshot, statusError) {
  var state = snapshot || {}
  var ready = !statusError && state.connectionState === "ready"
  var running = ready && state.isRunning === true
  var speed = Math.max(0, number((state.status || {}).speed, 0))
  var target = Math.max(0.5, Math.min(6, number(state.targetSpeed, 2.5)))
  var title = "Disconnected"
  if (statusError) title = "Service unavailable"
  else if (ready) title = running ? "Walking" : "Ready to walk"
  else if (state.connectionState === "scanning") title = "Looking for WalkingPad"
  else if (state.connectionState === "connecting" || state.connectionState === "connected") title = "Connecting"
  return {
    ready: ready,
    running: running,
    speed: speed,
    target: target,
    title: title,
    atTarget: running && Math.abs(speed - target) < 0.05,
    speedText: ready ? speed.toFixed(1) + " km/h" : "—",
    timeText: durationText(state.sessionTime),
    distanceText: Math.max(0, number(state.sessionDistance, 0)).toFixed(2) + " km",
    stepsText: String(Math.max(0, Math.floor(number(state.sessionSteps, 0))))
  }
}

function speedCommand(snapshot, statusError, busy, delta) {
  var view = presentation(snapshot, statusError)
  if (!view.ready || busy || !isFinite(delta)) return null
  var next = Math.max(0.5, Math.min(6, Math.round((view.target + delta) * 10) / 10))
  return next === view.target ? null : ["speed", next.toFixed(1)]
}
