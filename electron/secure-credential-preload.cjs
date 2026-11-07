 'use strict';
const {contextBridge,ipcRenderer}=require('electron');
// Main also verifies the exact app file URL, frame, WebContents and attempt.
if(process.isMainFrame&&location.protocol==='file:'&&location.pathname.endsWith('/secure-credential.html')) {
 contextBridge.exposeInMainWorld('adamPrivateInput',Object.freeze({
  credentials:(email,password)=>ipcRenderer.invoke('private-input:credentials',{email,password}),
  challenge:(challengeId,reply)=>ipcRenderer.invoke('private-input:challenge',{challengeId,reply}),
  view:()=>ipcRenderer.invoke('private-input:view'),
  cancel:()=>ipcRenderer.invoke('private-input:cancel'),
 }));
}
