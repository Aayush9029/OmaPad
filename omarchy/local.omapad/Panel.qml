import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Encouragements.js" as Encouragements

Panel {
  id: root
  moduleName: "local.omapad"
  ipcTarget: "local.omapad"
  manageIpc: false

  property var snapshot: ({
    connectionState: "disconnected",
    status: { speed: 0, mode: 2 },
    isRunning: false,
    targetSpeed: 2.5,
    sessionTime: 0,
    sessionDistance: 0,
    sessionSteps: 0
  })
  property string lastError: ""
  property bool cursorActive: false
  property int selectedAction: 0
  property real wheelAccumulator: 0
  property string encouragement: ""

  readonly property bool ready: String(snapshot.connectionState || "") === "ready"
  readonly property bool running: snapshot.isRunning === true
  readonly property real speed: Number((snapshot.status || {}).speed || 0)
  readonly property real targetSpeed: Number(snapshot.targetSpeed || 2.5)
  readonly property bool atTarget: running && Math.abs(speed - targetSpeed) < 0.05
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color muted: Qt.rgba(foreground.r, foreground.g, foreground.b, 0.58)
  readonly property color faint: Qt.rgba(foreground.r, foreground.g, foreground.b, 0.08)
  readonly property color accent: running ? "#2dd4bf" : (ready ? "#60a5fa" : muted)

  function commandFor(args) {
    return [String(settings.command || "omapad")].concat(args)
  }

  function refresh() {
    if (statusProc.running) return
    statusProc.command = commandFor(["status", "--json"])
    statusProc.running = true
  }

  function runAction(args) {
    if (actionProc.running || !ready) return
    lastError = ""
    actionProc.command = commandFor(args.concat(["--json"]))
    actionProc.running = true
  }

  function toggleRunning() { runAction([running ? "pause" : "start"]) }
  function stop() { runAction(["stop"]) }

  function adjustSpeed(delta) {
    var next = Math.max(0.5, Math.min(6.0, Math.round((targetSpeed + delta) * 10) / 10))
    if (next === targetSpeed) return
    snapshot.targetSpeed = next
    snapshot = Object.assign({}, snapshot)
    encouragement = ""
    runAction(["speed", next.toFixed(1)])
  }

  function pickEncouragement() {
    encouragement = atTarget ? Encouragements.forSpeed(speed) : ""
  }

  function durationText(seconds) {
    var total = Math.max(0, Math.floor(Number(seconds || 0)))
    var hours = Math.floor(total / 3600)
    var minutes = Math.floor((total % 3600) / 60)
    var secs = total % 60
    if (hours > 0) return hours + ":" + String(minutes).padStart(2, "0") + ":" + String(secs).padStart(2, "0")
    return minutes + ":" + String(secs).padStart(2, "0")
  }

  function stateText() {
    if (lastError !== "") return "Unavailable"
    if (atTarget && encouragement !== "") return encouragement
    if (ready) return "Target " + targetSpeed.toFixed(1) + " km/h"
    var state = String(snapshot.connectionState || "disconnected")
    return state.charAt(0).toUpperCase() + state.slice(1)
  }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  Component.onCompleted: refresh()
  onOpenedChanged: if (opened) {
    cursorActive = false
    selectedAction = 0
    pickEncouragement()
    refresh()
    Qt.callLater(function() { keyCatcher.forceActiveFocus() })
  }

  Timer {
    interval: Math.max(1000, Number(settings.refreshIntervalMs || 1000))
    running: true
    repeat: true
    onTriggered: root.refresh()
  }

  Process {
    id: statusProc
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        try {
          var parsed = JSON.parse(String(text || "{}"))
          if (parsed && parsed.status) {
            root.snapshot = parsed
            root.lastError = ""
            if (root.opened && root.atTarget && root.encouragement === "") root.pickEncouragement()
            else if (!root.atTarget) root.encouragement = ""
          }
        } catch (error) {
          root.lastError = "Invalid WalkingPad response"
        }
      }
    }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: if (String(text || "").trim() !== "") root.lastError = String(text).trim()
    }
  }

  Process {
    id: actionProc
    stdout: StdioCollector { waitForEnd: true }
    stderr: StdioCollector {
      waitForEnd: true
      onStreamFinished: if (String(text || "").trim() !== "") root.lastError = String(text).trim()
    }
    onRunningChanged: if (!running) root.refresh()
  }

  IpcHandler {
    target: root.ipcTarget
    function open(): void { root.open() }
    function close(): void { root.close() }
    function show(): void { root.open() }
    function hide(): void { root.close() }
    function toggle(): void { root.toggle() }
    function refresh(): string { root.refresh(); return "ok" }
    function start(): string { if (!root.running) root.toggleRunning(); return "ok" }
    function pause(): string { if (root.running) root.toggleRunning(); return "ok" }
    function stop(): string { root.stop(); return "ok" }
    function faster(): string { root.adjustSpeed(0.5); return "ok" }
    function slower(): string { root.adjustSpeed(-0.5); return "ok" }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: ""
    fontFamily: "JetBrainsMono Nerd Font"
    foreground: Color.muted
    activeColor: "#ffffff"
    active: root.running
    tooltipText: root.stateText()
    onPressed: function(mouseButton) {
      if (mouseButton === Qt.MiddleButton) root.refresh()
      else root.toggle()
    }
    onWheelMoved: function(delta) {
      if (!root.ready) return
      var wheel = Util.wheelSteps(root.wheelAccumulator, delta)
      root.wheelAccumulator = wheel.remainder
      if (wheel.steps !== 0) root.adjustSpeed(wheel.steps * 0.5)
    }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(360))
    contentHeight: panel.fittedContentHeight(content.implicitHeight, Style.space(480))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onMoveRequested: function(dx, dy) {
        root.cursorActive = true
        if (dx !== 0) root.adjustSpeed(dx * 0.5)
        else if (dy !== 0) root.selectedAction = Math.max(0, Math.min(1, root.selectedAction + dy))
      }
      onActivateRequested: if (root.selectedAction === 0) root.toggleRunning(); else root.stop()
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(text) {
        if (text === " " || text === "p" || text === "P") root.toggleRunning()
        else if (text === "s" || text === "S") root.stop()
        else if (text === "r" || text === "R") root.refresh()
      }

      Column {
        id: content
        width: parent.width
        spacing: Style.space(12)

        PanelHero {
          width: parent.width
          title: "OmaPad"
          meta: root.stateText()
          foreground: root.foreground
          fontFamily: bar ? bar.fontFamily : Style.font.family
          iconOpacity: root.ready ? 1.0 : 0.5
          iconComponent: Component {
            Text {
              text: ""
              color: root.accent
              font.family: "JetBrainsMono Nerd Font"
              font.pixelSize: Style.font.display
            }
          }
        }

        Rectangle {
          width: parent.width
          implicitHeight: speedColumn.implicitHeight + Style.space(24)
          radius: Style.space(12)
          color: root.faint

          Column {
            id: speedColumn
            anchors.fill: parent
            anchors.margins: Style.space(12)
            spacing: Style.space(8)

            RowLayout {
              width: parent.width
              Text {
                text: root.ready ? root.speed.toFixed(1) : "-"
                color: root.accent
                font.family: bar ? bar.fontFamily : Style.font.family
                font.pixelSize: Style.font.display
                font.bold: true
              }
              Text {
                text: "km/h"
                color: root.muted
                font.family: bar ? bar.fontFamily : Style.font.family
                font.pixelSize: Style.font.body
                Layout.alignment: Qt.AlignBottom
                Layout.bottomMargin: Style.space(4)
              }
              Item { Layout.fillWidth: true }
            }

            Rectangle {
              width: parent.width
              height: Style.space(5)
              radius: height / 2
              color: Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.1)
              Rectangle {
                width: parent.width * Math.max(0, Math.min(1, root.speed / 6))
                height: parent.height
                radius: height / 2
                color: root.accent
              }
            }

            RowLayout {
              width: parent.width
              Repeater {
                model: [
                  { label: "TIME", value: root.durationText(root.snapshot.sessionTime) },
                  { label: "DIST", value: Number(root.snapshot.sessionDistance || 0).toFixed(2) + " km" },
                  { label: "STEPS", value: String(root.snapshot.sessionSteps || 0) }
                ]
                delegate: Column {
                  Layout.fillWidth: true
                  Text {
                    text: modelData.label
                    color: root.muted
                    font.family: bar ? bar.fontFamily : Style.font.family
                    font.pixelSize: Style.font.caption
                    font.bold: true
                  }
                  Text {
                    text: modelData.value
                    color: root.foreground
                    font.family: bar ? bar.fontFamily : Style.font.family
                    font.pixelSize: Style.font.body
                    font.bold: true
                  }
                }
              }
            }
          }
        }

        RowLayout {
          width: parent.width
          spacing: Style.space(8)

          ActionButton {
            Layout.fillWidth: true
            text: root.running ? "Pause" : "Start"
            glyph: root.running ? "󰏤" : "󰐊"
            enabled: root.ready && !actionProc.running
            selected: root.cursorActive && root.selectedAction === 0
            destructive: false
            onClicked: root.toggleRunning()
          }
          ActionButton {
            Layout.fillWidth: true
            text: "Stop"
            glyph: "󰓛"
            enabled: root.ready && !actionProc.running
            selected: root.cursorActive && root.selectedAction === 1
            destructive: true
            onClicked: root.stop()
          }
        }

        Text {
          visible: root.lastError !== "" || root.snapshot.error !== undefined
          width: parent.width
          text: root.lastError !== "" ? root.lastError : String(root.snapshot.error || "")
          color: "#fb7185"
          wrapMode: Text.Wrap
          maximumLineCount: 2
          elide: Text.ElideRight
          font.family: bar ? bar.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
        }

        Text {
          width: parent.width
          text: "Scroll or h/l to change speed  ·  Space to start/pause"
          color: root.muted
          horizontalAlignment: Text.AlignHCenter
          font.family: bar ? bar.fontFamily : Style.font.family
          font.pixelSize: Style.font.caption
        }
      }
    }
  }

  component ActionButton: Rectangle {
    id: action
    property string text: ""
    property string glyph: ""
    property bool selected: false
    property bool destructive: false
    signal clicked()

    implicitHeight: Style.space(42)
    radius: Style.space(10)
    color: mouse.containsMouse || selected
      ? Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.14)
      : root.faint
    border.width: selected ? 1 : 0
    border.color: destructive ? "#fb7185" : root.accent
    opacity: enabled ? 1 : 0.45

    Row {
      anchors.centerIn: parent
      spacing: Style.space(7)
      Text {
        text: action.glyph
        color: action.destructive ? "#fb7185" : root.foreground
        font.family: bar ? bar.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
      }
      Text {
        text: action.text
        color: action.destructive ? "#fb7185" : root.foreground
        font.family: bar ? bar.fontFamily : Style.font.family
        font.pixelSize: Style.font.body
        font.bold: true
      }
    }
    MouseArea {
      id: mouse
      anchors.fill: parent
      hoverEnabled: true
      enabled: action.enabled
      cursorShape: Qt.PointingHandCursor
      onClicked: action.clicked()
    }
  }
}
