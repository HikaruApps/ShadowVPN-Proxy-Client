//! A private JSON-lines channel to the Go core. No TCP control port or shell commands.
use serde_json::{json, Value};
use std::{
    collections::{HashMap, VecDeque},
    io::{BufRead, BufReader, Write},
    path::Path,
    process::{Child, ChildStdin, Command, Stdio},
    sync::{
        atomic::{AtomicBool, AtomicU64, Ordering},
        mpsc, Arc, Condvar, Mutex,
    },
    thread,
    time::{Duration, Instant},
};

type Pending = Arc<Mutex<HashMap<u64, mpsc::Sender<Result<Value, String>>>>>;
type LogBuffer = Arc<Mutex<VecDeque<String>>>;
type LogCallback = Arc<dyn Fn(String) + Send + Sync>;
type ProfileCallback = Arc<dyn Fn(Value) + Send + Sync>;
type TrafficCallback = Arc<dyn Fn(Value) + Send + Sync>;
const MAX_LOG_LINES: usize = 1500;
// The Go core arms its own 20 s exit watchdog when stdin closes; give it a
// little longer before terminating it from here.
const GRACEFUL_EXIT: Duration = Duration::from_secs(25);

pub struct Backend {
    stdin: Mutex<Option<ChildStdin>>,
    pending: Pending,
    next_id: AtomicU64,
    busy: AtomicBool,
    finished: Arc<(Mutex<bool>, Condvar)>,
    child: Arc<Mutex<Child>>,
    logs: LogBuffer,
    pub state: Arc<Mutex<String>>,
}

struct BusyGuard<'a>(&'a AtomicBool);
impl Drop for BusyGuard<'_> {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

fn redact_marker(mut value: String, marker: &str) -> String {
    let mut offset = 0;
    while let Some(relative) = value[offset..].find(marker) {
        let start = offset + relative + marker.len();
        let Some(end_relative) = value[start..].find('"') else {
            break;
        };
        let end = start + end_relative;
        value.replace_range(start..end, "***");
        offset = start + 3;
    }
    value
}

fn sanitize_log_line(line: &str) -> String {
    let mut value: String = line.trim_end().chars().take(4000).collect();
    for scheme in ["vless://", "trojan://", "vmess://", "ss://"] {
        while let Some(start) = value.find(scheme) {
            let end = value[start..]
                .find(char::is_whitespace)
                .map(|position| start + position)
                .unwrap_or(value.len());
            value.replace_range(start..end, "<redacted-server-uri>");
        }
    }
    for marker in [
        "\"id\":\"",
        "\"password\":\"",
        "\"publicKey\":\"",
        "\"shortId\":\"",
    ] {
        value = redact_marker(value, marker);
    }
    value
}

fn record_log(logs: &LogBuffer, callback: &LogCallback, line: impl Into<String>) {
    let line = sanitize_log_line(&line.into());
    if line.is_empty() {
        return;
    }
    {
        let mut buffer = logs.lock().unwrap();
        if buffer.len() >= MAX_LOG_LINES {
            buffer.pop_front();
        }
        buffer.push_back(line.clone());
    }
    callback(line);
}

fn normalized_traffic(value: &Value) -> Option<Value> {
    let traffic = value.as_object()?;
    let upload_bytes = traffic.get("uploadBytes")?.as_u64()?;
    let download_bytes = traffic.get("downloadBytes")?.as_u64()?;
    let upload_bps = traffic.get("uploadBps")?.as_u64()?;
    let download_bps = traffic.get("downloadBps")?.as_u64()?;
    Some(json!({
        "uploadBytes": upload_bytes,
        "downloadBytes": download_bytes,
        "uploadBps": upload_bps,
        "downloadBps": download_bps,
    }))
}

/// A core that hung (or whose parent crashed) keeps the Wintun adapter and the
/// Kill Switch WFP session alive, which blocks all traffic for the new core
/// until a reboot. The app is single-instance, so any running core is stale.
#[cfg(windows)]
pub fn terminate_stale_cores(executable: &Path) -> bool {
    use std::os::windows::process::CommandExt;
    let Some(name) = executable.file_name().and_then(|name| name.to_str()) else {
        return false;
    };
    let killed = Command::new("taskkill")
        .args(["/F", "/T", "/IM", name])
        .stdin(Stdio::null())
        .stdout(Stdio::null())
        .stderr(Stdio::null())
        .creation_flags(0x08000000)
        .status()
        .map(|status| status.success())
        .unwrap_or(false);
    if killed {
        // Let Windows tear down the process-owned adapter and WFP session.
        thread::sleep(Duration::from_millis(800));
    }
    killed
}

#[cfg(not(windows))]
pub fn terminate_stale_cores(_executable: &Path) -> bool {
    false
}

impl Backend {
    pub fn spawn(
        executable: &Path,
        on_state: impl Fn(String) + Send + Sync + 'static,
        on_profile: impl Fn(Value) + Send + Sync + 'static,
        on_traffic: impl Fn(Value) + Send + Sync + 'static,
        on_log: impl Fn(String) + Send + Sync + 'static,
        on_exit: impl Fn() + Send + Sync + 'static,
    ) -> Result<Arc<Self>, String> {
        let mut command = Command::new(executable);
        command
            .current_dir(executable.parent().ok_or("Не найдена папка ядра")?)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000); // CREATE_NO_WINDOW; preserve inherited UAC token.
        }
        let mut child = command
            .spawn()
            .map_err(|_| "Не удалось запустить bin/shadowvpn-core.exe".to_string())?;
        let stdin = child.stdin.take().ok_or("Нет канала команд ядра")?;
        let stdout = child.stdout.take().ok_or("Нет канала ответов ядра")?;
        let stderr = child.stderr.take().ok_or("Нет канала журналов ядра")?;
        let process_id = child.id();
        let child = Arc::new(Mutex::new(child));
        let pending: Pending = Arc::new(Mutex::new(HashMap::new()));
        let state = Arc::new(Mutex::new("disconnected".to_string()));
        let finished = Arc::new((Mutex::new(false), Condvar::new()));
        let logs: LogBuffer = Arc::new(Mutex::new(VecDeque::with_capacity(MAX_LOG_LINES)));
        let log_callback: LogCallback = Arc::new(on_log);
        let backend = Arc::new(Self {
            stdin: Mutex::new(Some(stdin)),
            pending: pending.clone(),
            next_id: AtomicU64::new(1),
            busy: AtomicBool::new(false),
            finished: finished.clone(),
            child: child.clone(),
            logs: logs.clone(),
            state: state.clone(),
        });
        record_log(
            &logs,
            &log_callback,
            format!("[bridge] Go core process started pid={process_id}"),
        );
        let state_callback = Arc::new(on_state);
        let profile_callback: ProfileCallback = Arc::new(on_profile);
        let traffic_callback: TrafficCallback = Arc::new(on_traffic);
        let stderr_logs = logs.clone();
        let stderr_callback = log_callback.clone();
        thread::spawn(move || {
            for line in BufReader::new(stderr).lines() {
                match line {
                    Ok(line) => record_log(&stderr_logs, &stderr_callback, line),
                    Err(_) => break,
                }
            }
        });
        let stdout_logs = logs.clone();
        let stdout_log_callback = log_callback.clone();
        thread::spawn(move || {
            for line in BufReader::new(stdout).lines() {
                let Ok(line) = line else { break };
                let Ok(msg) = serde_json::from_str::<Value>(&line) else {
                    record_log(
                        &stdout_logs,
                        &stdout_log_callback,
                        "[bridge] Ignored malformed IPC output from Go core",
                    );
                    continue;
                };
                if msg["event"] == "state" {
                    if let Some(s) = msg["state"].as_str().filter(|s| {
                        ["disconnected", "connecting", "connected", "disconnecting"].contains(s)
                    }) {
                        *state.lock().unwrap() = s.to_string();
                        state_callback(s.to_string());
                    }
                }
                if msg["event"] == "profile" {
                    if let Some(profile) = msg.get("profile").filter(|value| value.is_object()) {
                        profile_callback(profile.clone());
                    }
                }
                if msg["event"] == "traffic" {
                    if let Some(traffic) = msg.get("traffic").and_then(normalized_traffic) {
                        traffic_callback(traffic);
                    }
                }
                if let Some(id) = msg["id"].as_u64() {
                    if let Some(sender) = pending.lock().unwrap().remove(&id) {
                        let reply = if msg["ok"] == true {
                            Ok(msg["result"].clone())
                        } else {
                            Err(msg["error"].as_str().unwrap_or("Ошибка ядра").to_string())
                        };
                        let _ = sender.send(reply);
                    }
                }
            }
            // Drain all outstanding callers even after an unexpected core exit.
            for (_, sender) in pending.lock().unwrap().drain() {
                let _ = sender.send(Err("Ядро завершилось. Перезапустите приложение".into()));
            }
            *state.lock().unwrap() = "disconnected".into();
            state_callback("disconnected".into());
        });
        let exit_logs = logs.clone();
        let exit_callback = log_callback.clone();
        thread::spawn(move || {
            // Poll instead of blocking in wait() so shutdown can still kill the child.
            let status = loop {
                match child.lock().unwrap().try_wait() {
                    Ok(Some(status)) => break Ok(status),
                    Ok(None) => {}
                    Err(error) => break Err(error),
                }
                thread::sleep(Duration::from_millis(100));
            };
            record_log(
                &exit_logs,
                &exit_callback,
                format!("[bridge] Go core process exited status={status:?}"),
            );
            let (done, signal) = &*finished;
            *done.lock().unwrap() = true;
            signal.notify_all();
            on_exit();
        });
        Ok(backend)
    }

    fn send_request(&self, method: &str, mut data: Value) -> Result<Value, String> {
        let id = self.next_id.fetch_add(1, Ordering::Relaxed);
        data["id"] = json!(id);
        data["method"] = json!(method);
        let mut line = serde_json::to_vec(&data).map_err(|_| "Не удалось создать команду")?;
        line.push(b'\n');
        let (tx, rx) = mpsc::channel();
        self.pending.lock().unwrap().insert(id, tx);
        let write_result = {
            let mut input = self.stdin.lock().unwrap();
            input
                .as_mut()
                .ok_or_else(|| "Ядро остановлено. Перезапустите приложение".to_string())
                .and_then(|pipe| {
                    pipe.write_all(&line)
                        .map_err(|_| "Соединение с ядром потеряно".to_string())
                })
        };
        if let Err(e) = write_result {
            self.pending.lock().unwrap().remove(&id);
            return Err(e);
        }
        // The first connection may also bootstrap and validate custom GeoData.
        let timeout = if method == "connect" { 180 } else { 60 };
        match rx.recv_timeout(Duration::from_secs(timeout)) {
            Ok(reply) => reply,
            Err(_) => {
                self.pending.lock().unwrap().remove(&id);
                self.stdin.lock().unwrap().take();
                let finished = self.finished.clone();
                let child = self.child.clone();
                thread::spawn(move || {
                    if !wait_finished(&finished, GRACEFUL_EXIT) {
                        let _ = child.lock().unwrap().kill();
                    }
                });
                Err("Ядро не ответило вовремя и будет перезапущено. Повторите действие".into())
            }
        }
    }

    pub fn request(&self, method: &str, data: Value) -> Result<Value, String> {
        if self.busy.swap(true, Ordering::AcqRel) {
            return Err("Дождитесь текущей операции".into());
        }
        let _guard = BusyGuard(&self.busy);
        self.send_request(method, data)
    }

    pub fn query(&self, method: &str, data: Value) -> Result<Value, String> {
        self.send_request(method, data)
    }

    pub fn logs(&self) -> Vec<String> {
        self.logs.lock().unwrap().iter().cloned().collect()
    }

    /// Carries the diagnostic log over to a restarted core.
    pub fn prepend_logs(&self, lines: Vec<String>) {
        let mut buffer = self.logs.lock().unwrap();
        for line in lines.into_iter().rev() {
            if buffer.len() >= MAX_LOG_LINES {
                break;
            }
            buffer.push_front(line);
        }
    }

    pub fn clear_logs(&self) {
        self.logs.lock().unwrap().clear();
    }

    pub fn is_finished(&self) -> bool {
        *self.finished.0.lock().unwrap()
    }

    pub fn shutdown(&self) -> Result<(), String> {
        // Closing stdin triggers context cancellation + Xray.Close in the Go worker.
        self.stdin.lock().unwrap().take();
        if wait_finished(&self.finished, GRACEFUL_EXIT) {
            return Ok(());
        }
        // A core that cannot stop would keep TUN and WFP state; terminating it
        // lets Windows release both instead of leaving them until a reboot.
        let _ = self.child.lock().unwrap().kill();
        if wait_finished(&self.finished, Duration::from_secs(5)) {
            Ok(())
        } else {
            Err("Ядро не удалось остановить. Выполните stop-shadowvpn.cmd".into())
        }
    }
}

fn wait_finished(finished: &(Mutex<bool>, Condvar), timeout: Duration) -> bool {
    let (done, signal) = finished;
    let deadline = Instant::now() + timeout;
    let mut complete = done.lock().unwrap();
    while !*complete {
        let remaining = deadline.saturating_duration_since(Instant::now());
        if remaining.is_zero() {
            return false;
        }
        complete = signal.wait_timeout(complete, remaining).unwrap().0;
    }
    true
}

#[cfg(all(test, unix))]
mod tests {
    use super::*;
    use std::{fs, os::unix::fs::PermissionsExt};
    #[test]
    fn sensitive_server_values_are_redacted() {
        let line = r#"config={"id":"secret-uuid","password":"secret"} vless://user@example.com:443?security=tls"#;
        let sanitized = sanitize_log_line(line);
        assert!(!sanitized.contains("secret-uuid"));
        assert!(!sanitized.contains("\"secret\""));
        assert!(!sanitized.contains("vless://"));
        assert!(sanitized.contains("<redacted-server-uri>"));
    }

    #[test]
    fn traffic_payload_is_strictly_normalized() {
        let valid = normalized_traffic(&json!({
            "uploadBytes": 1024,
            "downloadBytes": 2048,
            "uploadBps": 256,
            "downloadBps": 512,
            "ignored": "field",
        }))
        .unwrap();
        assert_eq!(valid["downloadBps"], 512);
        assert!(valid.get("ignored").is_none());
        assert!(normalized_traffic(&json!({"uploadBytes": -1})).is_none());
    }

    #[test]
    fn ipc_state_errors_and_graceful_eof() {
        let folder = std::env::temp_dir().join(format!("shadowvpn-ipc-{}", std::process::id()));
        fs::create_dir_all(&folder).unwrap();
        let file = folder.join("fake-core");
        fs::write(&file, "#!/usr/bin/env python3\nimport json,sys\nprint(json.dumps({'event':'ready'}),flush=True)\nfor line in sys.stdin:\n r=json.loads(line)\n print(json.dumps({'event':'state','state':'connected'}),flush=True)\n print(json.dumps({'event':'profile','profile':{'profileId':'server','name':'Berlin'}}),flush=True)\n print(json.dumps({'event':'traffic','traffic':{'uploadBytes':1024,'downloadBytes':2048,'uploadBps':256,'downloadBps':512}}),flush=True)\n print(json.dumps({'id':r['id'],'ok':r['method']!='bad','result':{'value':42},'error':'expected'}),flush=True)\n").unwrap();
        fs::set_permissions(&file, fs::Permissions::from_mode(0o700)).unwrap();
        let selected = Arc::new(Mutex::new(Value::Null));
        let selected_callback = selected.clone();
        let traffic = Arc::new(Mutex::new(Value::Null));
        let traffic_callback = traffic.clone();
        let backend = Backend::spawn(
            &file,
            |_| {},
            move |profile| {
                *selected_callback.lock().unwrap() = profile;
            },
            move |sample| {
                *traffic_callback.lock().unwrap() = sample;
            },
            |_| {},
            || {},
        )
        .unwrap();
        assert_eq!(backend.request("test", json!({})).unwrap()["value"], 42);
        assert_eq!(&*backend.state.lock().unwrap(), "connected");
        assert_eq!(selected.lock().unwrap()["profileId"], "server");
        assert_eq!(traffic.lock().unwrap()["downloadBps"], 512);
        assert_eq!(backend.request("bad", json!({})).unwrap_err(), "expected");
        backend.shutdown().unwrap();
        assert!(backend.request("test", json!({})).is_err());
        fs::remove_dir_all(folder).unwrap();
    }
}
