#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use serde_json::{json, Value};
use shadowvpn::backend::Backend;
use shadowvpn::settings::validate_dns_request;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use tauri::{Emitter, Manager, State};

const AUTOSTART_TASK_NAME: &str = "ShadowVPN Auto Start";

struct Service(Result<Arc<Backend>, String>);
#[derive(Default)]
struct Closing {
    active: AtomicBool,
    finished: AtomicBool,
}

async fn call(service: &Service, method: &'static str, data: Value) -> Result<Value, String> {
    let backend = service.0.clone()?;
    tauri::async_runtime::spawn_blocking(move || backend.request(method, data))
        .await
        .map_err(|_| "Ошибка потока ядра".to_string())?
}
async fn query(service: &Service, method: &'static str, data: Value) -> Result<Value, String> {
    let backend = service.0.clone()?;
    tauri::async_runtime::spawn_blocking(move || backend.query(method, data))
        .await
        .map_err(|_| "Ошибка потока ядра".to_string())?
}
#[tauri::command]
async fn vpn_import(url: String, reason: Option<String>, service: State<'_, Service>) -> Result<Value, String> {
    if url.len() > 8192 || !url.starts_with("https://") {
        return Err("Нужна HTTPS-ссылка на подписку".into());
    }
    let reason = match reason.as_deref() {
        Some("startup") => "startup",
        Some("automatic") => "automatic",
        _ => "manual",
    };
    call(&service, "import", json!({"url":url,"reason":reason})).await
}
#[tauri::command]
async fn vpn_connect(
    profile_id: String,
    dns: Option<String>,
    dns_servers: Option<Vec<String>>,
    fragmentation: Option<bool>,
    kill_switch: Option<bool>,
    auto_profile_ids: Option<Vec<String>>,
    route_mode: Option<String>,
    direct_domains: Option<Vec<String>>,
    service: State<'_, Service>,
) -> Result<Value, String> {
    if profile_id.len() != 24 || !profile_id.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err("Выберите сервер".into());
    }
    let auto_profile_ids = auto_profile_ids.unwrap_or_default();
    let route_mode = route_mode.unwrap_or_else(|| "full".into());
    if !["full", "bypass", "proxy_only"].contains(&route_mode.as_str()) {
        return Err("Неизвестный режим маршрутизации".into());
    }
    let direct_domains = direct_domains.unwrap_or_default();
    if direct_domains.len() > 512 || direct_domains.iter().any(|domain| domain.len() > 255) {
        return Err("Слишком много правил маршрутизации".into());
    }
    if auto_profile_ids.len() > 512
        || auto_profile_ids.iter().any(|id| id.len() != 24 || !id.bytes().all(|b| b.is_ascii_hexdigit()) || id.bytes().all(|b| b == b'0'))
    {
        return Err("Некорректный состав Auto-группы".into());
    }
    if profile_id != "000000000000000000000000"
        && profile_id != "000000000000000000000001"
        && !auto_profile_ids.is_empty()
    {
        return Err("Группу можно использовать только с Auto".into());
    }
    let (dns, dns_servers) = validate_dns_request(dns, dns_servers)?;
    call(
        &service,
        "connect",
        json!({"profileId":profile_id,"dns":dns,"dnsServers":dns_servers,"fragmentation":fragmentation.unwrap_or(false),"killSwitch":kill_switch.unwrap_or(false),"autoProfileIds":auto_profile_ids,"routeMode":route_mode,"directDomains":direct_domains}),
    )
    .await
}
#[tauri::command]
async fn vpn_disconnect(service: State<'_, Service>) -> Result<Value, String> {
    call(&service, "disconnect", json!({})).await
}
#[tauri::command]
async fn vpn_ping(ping_method: Option<String>, service: State<'_, Service>) -> Result<Value, String> {
    let ping_method = match ping_method.as_deref() {
        Some("head") => "head",
        Some("get") => "get",
        _ => "tcp",
    };
    call(&service, "ping", json!({"pingMethod":ping_method})).await
}
#[tauri::command]
async fn vpn_public_ip(masked: bool, service: State<'_, Service>) -> Result<Value, String> {
    query(&service, "publicIp", json!({"masked":masked})).await
}
#[tauri::command]
async fn vpn_device_info(service: State<'_, Service>) -> Result<Value, String> {
    query(&service, "deviceInfo", json!({})).await
}
#[tauri::command]
fn vpn_get_autostart() -> Result<bool, String> {
    #[cfg(windows)]
    {
        return Ok(std::process::Command::new("schtasks.exe")
            .args(["/Query", "/TN", AUTOSTART_TASK_NAME])
            .output()
            .map_err(|_| "Не удалось проверить автозапуск Windows".to_string())?
            .status
            .success());
    }
    #[cfg(not(windows))]
    Err("Автозапуск поддерживается только в Windows".into())
}
#[tauri::command]
fn vpn_set_autostart(enabled: bool) -> Result<bool, String> {
    #[cfg(windows)]
    {
        let output = if enabled {
            let executable = std::env::current_exe().map_err(|_| "Не удалось определить путь ShadowVPN".to_string())?;
            let command = format!("\"{}\"", executable.display());
            std::process::Command::new("schtasks.exe")
                .args(["/Create", "/TN", AUTOSTART_TASK_NAME, "/SC", "ONLOGON", "/TR", &command, "/RL", "HIGHEST", "/F"])
                .output()
        } else {
            std::process::Command::new("schtasks.exe")
                .args(["/Delete", "/TN", AUTOSTART_TASK_NAME, "/F"])
                .output()
        }
        .map_err(|_| "Не удалось изменить автозапуск Windows".to_string())?;
        if !output.status.success() {
            return Err("Windows не разрешила изменить автозапуск".into());
        }
        Ok(enabled)
    }
    #[cfg(not(windows))]
    Err("Автозапуск поддерживается только в Windows".into())
}
#[tauri::command]
fn vpn_get_state(service: State<'_, Service>) -> String {
    service
        .0
        .as_ref()
        .map(|b| b.state.lock().unwrap().clone())
        .unwrap_or_else(|_| "disconnected".into())
}
#[tauri::command]
fn vpn_get_logs(service: State<'_, Service>) -> Result<Vec<String>, String> {
    let backend = service.0.as_ref().map_err(Clone::clone)?;
    Ok(backend.logs())
}
#[tauri::command]
fn vpn_clear_logs(service: State<'_, Service>) -> Result<(), String> {
    let backend = service.0.as_ref().map_err(Clone::clone)?;
    backend.clear_logs();
    Ok(())
}

fn request_exit(app: tauri::AppHandle) {
    if app.state::<Closing>().active.swap(true, Ordering::AcqRel) {
        return;
    }
    let backend = app.state::<Service>().0.clone();
    tauri::async_runtime::spawn(async move {
        let result = if let Ok(backend) = backend {
            tauri::async_runtime::spawn_blocking(move || backend.shutdown())
                .await
                .unwrap_or_else(|_| Err("Ошибка завершения ядра".into()))
        } else {
            Ok(())
        };
        match result {
            Ok(()) => {
                app.state::<Closing>()
                    .finished
                    .store(true, Ordering::Release);
                app.exit(0);
            }
            Err(e) => {
                app.state::<Closing>()
                    .active
                    .store(false, Ordering::Release);
                let _ = app.emit("vpn:error", e);
            }
        }
    });
}
fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            if let Some(window) = app.get_webview_window("main") {
                let _ = window.unminimize();
                let _ = window.set_focus();
            }
        }))
        .manage(Closing::default())
        .setup(|app| {
            let name = if cfg!(windows) {
                "shadowvpn-core.exe"
            } else {
                "shadowvpn-core"
            };
            let directory = if cfg!(debug_assertions) {
                std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../bin")
            } else {
                app.path().resource_dir()?.join("bin")
            };
            let state_handle = app.handle().clone();
            let profile_handle = app.handle().clone();
            let log_handle = app.handle().clone();
            let service = Backend::spawn(
                &directory.join(name),
                move |state| {
                    let _ = state_handle.emit("vpn:state", state);
                },
                move |profile| {
                    let _ = profile_handle.emit("vpn:profile", profile);
                },
                move |line| {
                    let _ = log_handle.emit("vpn:log", line);
                },
            );
            app.manage(Service(service));
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            vpn_import,
            vpn_connect,
            vpn_disconnect,
            vpn_ping,
            vpn_public_ip,
            vpn_device_info,
            vpn_get_autostart,
            vpn_set_autostart,
            vpn_get_state,
            vpn_get_logs,
            vpn_clear_logs
        ])
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if !window
                    .app_handle()
                    .state::<Closing>()
                    .finished
                    .load(Ordering::Acquire)
                {
                    api.prevent_close();
                    request_exit(window.app_handle().clone());
                }
            }
        })
        .build(tauri::generate_context!())
        .expect("Не удалось запустить ShadowVPN")
        .run(|app, event| {
            if let tauri::RunEvent::ExitRequested { api, .. } = event {
                if !app.state::<Closing>().finished.load(Ordering::Acquire) {
                    api.prevent_exit();
                    request_exit(app.clone());
                }
            }
        });
}
