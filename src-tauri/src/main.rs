#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use serde_json::{json, Value};
use shadowvpn::backend::Backend;
use shadowvpn::settings::validate_dns_request;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use tauri::{Emitter, Manager, State};

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
async fn vpn_import(url: String, service: State<'_, Service>) -> Result<Value, String> {
    if url.len() > 8192 || !url.starts_with("https://") {
        return Err("Нужна HTTPS-ссылка на подписку".into());
    }
    call(&service, "import", json!({"url":url})).await
}
#[tauri::command]
async fn vpn_connect(
    profile_id: String,
    dns: Option<String>,
    dns_servers: Option<Vec<String>>,
    fragmentation: Option<bool>,
    kill_switch: Option<bool>,
    service: State<'_, Service>,
) -> Result<Value, String> {
    if profile_id.len() != 24 || !profile_id.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err("Выберите сервер".into());
    }
    let (dns, dns_servers) = validate_dns_request(dns, dns_servers)?;
    call(
        &service,
        "connect",
        json!({"profileId":profile_id,"dns":dns,"dnsServers":dns_servers,"fragmentation":fragmentation.unwrap_or(false),"killSwitch":kill_switch.unwrap_or(false)}),
    )
    .await
}
#[tauri::command]
async fn vpn_disconnect(service: State<'_, Service>) -> Result<Value, String> {
    call(&service, "disconnect", json!({})).await
}
#[tauri::command]
async fn vpn_ping(service: State<'_, Service>) -> Result<Value, String> {
    call(&service, "ping", json!({})).await
}
#[tauri::command]
async fn vpn_public_ip(masked: bool, service: State<'_, Service>) -> Result<Value, String> {
    query(&service, "publicIp", json!({"masked":masked})).await
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
            let log_handle = app.handle().clone();
            let service = Backend::spawn(
                &directory.join(name),
                move |state| {
                    let _ = state_handle.emit("vpn:state", state);
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
