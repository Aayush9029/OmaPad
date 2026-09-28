import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Encouragements.js" as Encouragements
import "Model.js" as Model

Panel {
  id: root
  moduleName: "io.github.aayush9029.omapad"
  ipcTarget: "io.github.aayush9029.omapad"
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
  property string statusError: ""
  property bool cursorActive: false
  property int selectedAction: 0
  property real wheelAccumulator: 0
  property string encouragement: ""

  readonly property var view: Model.presentation(snapshot, statusError)
  readonly property bool ready: view.ready
  readonly property bool running: view.running
  readonly property real speed: view.speed
  readonly property real targetSpeed: view.target
  readonly property bool atTarget: view.atTarget
  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color muted: Qt.darker(foreground, 1.55)
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property string errorText: statusError || lastError || String(snapshot.error || "")

  function commandFor(args) {
    return [String(settings.command || "omapad")].concat(args)
  }

  function refresh() {
    if (statusProc.running || actionProc.running) return
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
    var args = Model.speedCommand(snapshot, statusError, actionProc.running, delta)
    if (args) runAction(args)
  }

  function pickEncouragement() {
    encouragement = atTarget ? Encouragements.forSpeed(speed) : ""
  }

  function stateText() {
    return atTarget && encouragement !== "" ? encouragement : view.title
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
    stdout: StdioCollector { id: statusOutput; waitForEnd: true }
    stderr: StdioCollector { id: statusErrors; waitForEnd: true }
    onExited: function(exitCode, exitStatus) {
      if (exitCode !== 0 || exitStatus !== 0) {
        root.statusError = String(statusErrors.text || "").trim() || "OmaPad service is unavailable"
        return
      }
      try {
        root.snapshot = Model.parseSnapshot(String(statusOutput.text || ""))
        root.statusError = ""
        if (root.opened && root.atTarget && root.encouragement === "") root.pickEncouragement()
        else if (!root.atTarget) root.encouragement = ""
      } catch (error) {
        root.statusError = "Invalid WalkingPad response"
      }
    }
  }

  Process {
    id: actionProc
    stdout: StdioCollector { waitForEnd: true }
    stderr: StdioCollector { id: actionErrors; waitForEnd: true }
    onExited: function(exitCode, exitStatus) {
      if (exitCode !== 0 || exitStatus !== 0)
        root.lastError = String(actionErrors.text || "").trim() || "WalkingPad command failed"
      root.refresh()
    }
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
    foreground: root.ready ? root.barForeground : Qt.darker(root.barForeground, 1.55)
    activeColor: root.barForeground
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
    contentWidth: panel.fittedContentWidth(Style.space(380))
    contentHeight: panel.fittedContentHeight(content.implicitHeight, Style.space(560))

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
        else if (text === "r" || text === "R") { root.lastError = ""; root.refresh() }
      }

      Flickable {
        anchors.fill: parent
        contentHeight: content.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds

        Column {
          id: content
          width: parent.width
          spacing: Style.space(12)

          PanelHero {
            width: parent.width
            title: "OmaPad"
            meta: root.stateText()
            foreground: root.foreground
            fontFamily: root.fontFamily
            iconOpacity: root.ready ? 1.0 : 0.5
            iconComponent: Component {
              Text {
                text: ""
                color: root.foreground
                font.family: "JetBrainsMono Nerd Font"
                font.pixelSize: Style.font.display
              }
            }
            trailingControl: Component {
              Button {
                iconText: "󰑓"
                tooltipText: "Refresh connection"
                foreground: root.foreground
                fontFamily: root.fontFamily
                enabled: !statusProc.running && !actionProc.running
                opacity: enabled ? 1 : 0.45
                onClicked: { root.lastError = ""; root.refresh() }
              }
            }
          }

          PanelSeparator { foreground: root.foreground }

          Column {
            width: parent.width
            spacing: Style.space(8)
            PanelSectionHeader {
              text: "SESSION"
              foreground: root.foreground
              fontFamily: root.fontFamily
            }
            MetricRow { label: "Speed"; value: root.view.speedText }
            MetricRow { label: "Time"; value: root.view.timeText }
            MetricRow { label: "Distance"; value: root.view.distanceText }
            MetricRow { label: "Steps"; value: root.view.stepsText }
          }

          PanelSeparator { foreground: root.foreground }

          Column {
            width: parent.width
            spacing: Style.space(10)
            PanelSectionHeader {
              text: "TARGET SPEED"
              foreground: root.foreground
              fontFamily: root.fontFamily
            }
            RowLayout {
              width: parent.width
              spacing: Style.space(12)
              ControlButton {
                text: "−"
                tooltipText: "Decrease speed by 0.5 km/h"
                enabled: root.ready && !actionProc.running && root.targetSpeed > 0.5
                onClicked: root.adjustSpeed(-0.5)
              }
              Text {
                Layout.fillWidth: true
                text: root.targetSpeed.toFixed(1) + " km/h"
                horizontalAlignment: Text.AlignHCenter
                color: root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                font.bold: true
              }
              ControlButton {
                text: "+"
                tooltipText: "Increase speed by 0.5 km/h"
                enabled: root.ready && !actionProc.running && root.targetSpeed < 6
                onClicked: root.adjustSpeed(0.5)
              }
            }
          }

          RowLayout {
            width: parent.width
            spacing: Style.space(8)
            ControlButton {
              Layout.fillWidth: true
              text: root.running ? "Pause" : "Start"
              iconText: root.running ? "󰏤" : "󰐊"
              active: root.ready
              hasCursor: root.cursorActive && root.selectedAction === 0
              onHovered: function(hovered) { if (hovered) { root.cursorActive = true; root.selectedAction = 0 } }
              onClicked: root.toggleRunning()
            }
            ControlButton {
              Layout.fillWidth: true
              text: "Stop"
              iconText: "󰓛"
              tooltipText: "Stop and reset this session"
              hasCursor: root.cursorActive && root.selectedAction === 1
              onHovered: function(hovered) { if (hovered) { root.cursorActive = true; root.selectedAction = 1 } }
              onClicked: root.stop()
            }
          }

          PanelSeparator { foreground: root.foreground }

          Text {
            width: parent.width
            visible: !root.ready || root.errorText !== ""
            textFormat: Text.PlainText
            text: root.errorText !== "" ? root.errorText : "Turn on your WalkingPad to connect."
            color: root.errorText !== "" ? Color.urgent : root.muted
            wrapMode: Text.Wrap
            font.family: root.fontFamily
            font.pixelSize: Style.font.bodySmall
          }
          Text {
            width: parent.width
            text: "← →  Speed   ·   Space  Select   ·   R  Refresh"
            color: root.muted
            wrapMode: Text.Wrap
            font.family: root.fontFamily
            font.pixelSize: Style.font.caption
          }
        }
      }
    }
  }

  component MetricRow: RowLayout {
    property string label: ""
    property string value: ""
    width: parent.width
    spacing: Style.space(20)
    Text {
      text: label
      color: root.muted
      font.family: root.fontFamily
      font.pixelSize: Style.font.bodySmall
    }
    Text {
      Layout.fillWidth: true
      text: value
      textFormat: Text.PlainText
      color: root.foreground
      horizontalAlignment: Text.AlignRight
      font.family: root.fontFamily
      font.pixelSize: Style.font.bodySmall
      elide: Text.ElideRight
    }
  }

  component ControlButton: Button {
    foreground: root.foreground
    fontFamily: root.fontFamily
    fontSize: Style.font.bodySmall
    verticalPadding: Style.spacing.controlPaddingY + Style.space(2)
    bordered: true
    enabled: root.ready && !actionProc.running
    opacity: enabled ? 1 : 0.4
  }
}
