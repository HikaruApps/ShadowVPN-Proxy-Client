const { contextBridge, ipcRenderer } = require("electron");
const { serverProfiles, groupServerProfiles, protocolLabel } = require("../shared/servers.js");

// Renderer runs with contextIsolation:true / nodeIntegration:false, so it
// has no `require()` of its own -- everything it needs crosses this one
// bridge file, same principle as VpnNativeBridge.ts on the Android side.
contextBridge.exposeInMainWorld("serverData", {
  profiles: serverProfiles,
  groupServerProfiles,
  protocolLabel,
});

contextBridge.exposeInMainWorld("vpnApi", {
  connect: (profile) => ipcRenderer.invoke("vpn:connect", profile),
  disconnect: () => ipcRenderer.invoke("vpn:disconnect"),
  getState: () => ipcRenderer.invoke("vpn:getState"),
  onStateChange: (callback) => {
    const listener = (_event, state) => callback(state);
    ipcRenderer.on("vpn:state", listener);
    return () => ipcRenderer.removeListener("vpn:state", listener);
  },
});
