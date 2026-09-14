const { app, BrowserWindow, ipcMain } = require("electron");
const path = require("path");
// const { spawn } = require("child_process"); // uncomment when wiring the real core

let mainWindow;
let connectionState = "disconnected";
// let coreProcess = null; // handle to the running xray-core child process

function createWindow() {
  mainWindow = new BrowserWindow({
    width: 900,
    height: 560,
    useContentSize: true,
    resizable: false,
    backgroundColor: "#000000",
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      nodeIntegration: false,
      // Electron 20+ defaults preload scripts to a restricted sandbox that
      // can't require() local project files (only a few whitelisted node
      // builtins). We still want the renderer itself isolated -- just not
      // the preload script, which is the one place we intentionally trust
      // with node access, same role as VpnBridgeModule.java on Android.
      sandbox: false,
    },
  });

  mainWindow.setMenu(null);
  mainWindow.loadFile(path.join(__dirname, "..", "renderer", "index.html"));
}

app.whenReady().then(createWindow);

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});

/**
 * Same seam as VpnCoreBackend on Android: this is where a real
 * implementation would spawn xray-core as a child process, write its
 * translated JSON config to a temp file, and manage the TUN interface
 * via a small native helper for the current OS.
 *
 *   coreProcess = spawn('xray', ['-c', configPath]);
 *
 * Stubbed for now so the UI is fully wireable and testable first.
 */
ipcMain.handle("vpn:connect", async (_event, profile) => {
  connectionState = "connecting";
  mainWindow.webContents.send("vpn:state", connectionState);

  await new Promise((r) => setTimeout(r, 600)); // simulate handshake

  connectionState = "connected";
  mainWindow.webContents.send("vpn:state", connectionState);
  return { ok: true };
});

ipcMain.handle("vpn:disconnect", async () => {
  connectionState = "disconnecting";
  mainWindow.webContents.send("vpn:state", connectionState);

  await new Promise((r) => setTimeout(r, 300));
  // if (coreProcess) { coreProcess.kill(); coreProcess = null; }

  connectionState = "disconnected";
  mainWindow.webContents.send("vpn:state", connectionState);
  return { ok: true };
});

ipcMain.handle("vpn:getState", () => connectionState);
