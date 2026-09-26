#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
use serde_json::{json, Value};
use shadowvpn::backend::Backend;
use shadowvpn::settings::{valid_https_endpoint, validate_dns_request};
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc,
};
use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    Emitter, Manager, State,
};
use tauri_plugin_updater::UpdaterExt;

const AUTOSTART_TASK_NAME: &str = "ShadowVPN Auto Start";

struct Service(Result<Arc<Backend>, String>);
#[derive(Default)]
struct Closing {
    active: AtomicBool,
    finished: AtomicBool,
}
#[derive(Default)]
struct Updating(Arc<AtomicBool>);
struct UpdateGuard(Arc<AtomicBool>);

impl Drop for UpdateGuard {
    fn drop(&mut self) {
        self.0.store(false, Ordering::Release);
    }
}

fn begin_update(updating: &Updating) -> Result<UpdateGuard, String> {
    let active = updating.0.clone();
    active
        .compare_exchange(false, true, Ordering::AcqRel, Ordering::Acquire)
        .map_err(|_| "Проверка или установка обновления уже выполняется".to_string())?;
    Ok(UpdateGuard(active))
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

fn validate_geodata_url(value: Option<String>, title: &str) -> Result<String, String> {
    let value = value.unwrap_or_default().trim().to_string();
    if value.is_empty() {
        return Ok(value);
    }
    if value.len() > 2048
        || !value.starts_with("https://")
        || value.chars().any(char::is_whitespace)
        || value.contains('#')
    {
        return Err(format!("{title}: нужна корректная HTTPS-ссылка"));
    }
    Ok(value)
}
#[tauri::command]
async fn vpn_import(
    url: String,
    reason: Option<String>,
    service: State<'_, Service>,
) -> Result<Value, String> {
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
async fn vpn_import_many(
    urls: Vec<String>,
    reason: Option<String>,
    service: State<'_, Service>,
) -> Result<Value, String> {
    if urls.is_empty() || urls.len() > 16 {
        return Err("Добавьте от 1 до 16 подписок".into());
    }
    let total_length: usize = urls.iter().map(String::len).sum();
    if total_length > 48 * 1024
        || urls
            .iter()
            .any(|url| url.len() > 8192 || !url.starts_with("https://"))
    {
        return Err("Для каждой подписки нужна HTTPS-ссылка".into());
    }
    let reason = match reason.as_deref() {
        Some("startup") => "startup",
        Some("automatic") => "automatic",
        _ => "manual",
    };
    call(&service, "importMany", json!({"urls":urls,"reason":reason})).await
}
#[tauri::command]
fn vpn_open_url(url: String) -> Result<(), String> {
    if !valid_https_endpoint(&url, true) {
        return Err("Некорректная HTTPS-ссылка".into());
    }
    #[cfg(windows)]
    {
        use std::os::windows::process::CommandExt;
        const CREATE_NO_WINDOW: u32 = 0x08000000;
        std::process::Command::new("rundll32.exe")
            .args(["url.dll,FileProtocolHandler", &url])
            .creation_flags(CREATE_NO_WINDOW)
            .spawn()
            .map_err(|_| "Не удалось открыть ссылку".to_string())?;
        Ok(())
    }
    #[cfg(not(windows))]
    {
        let _ = url;
        Err("Открытие ссылок поддерживается только в Windows".into())
    }
}
#[tauri::command]
async fn vpn_connect(
    profile_id: String,
    dns: Option<String>,
    dns_servers: Option<Vec<String>>,
    dns_doh: Option<String>,
    fragmentation: Option<bool>,
    kill_switch: Option<bool>,
    auto_profile_ids: Option<Vec<String>>,
    route_mode: Option<String>,
    direct_domains: Option<Vec<String>>,
    geo_ip_url: Option<String>,
    geo_site_url: Option<String>,
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
    let (geo_ip_url, geo_site_url) = if route_mode == "full" {
        (String::new(), String::new())
    } else {
        (
            validate_geodata_url(geo_ip_url, "GeoIP")?,
            validate_geodata_url(geo_site_url, "GeoSite")?,
        )
    };
    if auto_profile_ids.len() > 512
        || auto_profile_ids.iter().any(|id| {
            id.len() != 24
                || !id.bytes().all(|b| b.is_ascii_hexdigit())
                || id.bytes().all(|b| b == b'0')
        })
    {
        return Err("Некорректный состав Auto-группы".into());
    }
    if profile_id != "000000000000000000000000"
        && profile_id != "000000000000000000000001"
        && !auto_profile_ids.is_empty()
    {
        return Err("Группу можно использовать только с Auto".into());
    }
    let (dns, dns_servers, dns_doh) = validate_dns_request(dns, dns_servers, dns_doh)?;
    call(
        &service,
        "connect",
        json!({"profileId":profile_id,"dns":dns,"dnsServers":dns_servers,"dnsDoh":dns_doh,"fragmentation":fragmentation.unwrap_or(false),"killSwitch":kill_switch.unwrap_or(false),"autoProfileIds":auto_profile_ids,"routeMode":route_mode,"directDomains":direct_domains,"geoIpUrl":geo_ip_url,"geoSiteUrl":geo_site_url}),
    )
    .await
}
#[tauri::command]
async fn vpn_disconnect(service: State<'_, Service>) -> Result<Value, String> {
    call(&service, "disconnect", json!({})).await
}
#[tauri::command]
async fn vpn_ping(
    ping_method: Option<String>,
    service: State<'_, Service>,
) -> Result<Value, String> {
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
async fn vpn_check_update(
    app: tauri::AppHandle,
    updating: State<'_, Updating>,
) -> Result<Value, String> {
    let _guard = begin_update(&updating)?;
    let current_version = app.package_info().version.to_string();
    let update = app
        .updater()
        .map_err(|error| format!("Не удалось запустить проверку обновлений: {error}"))?
        .check()
        .await
        .map_err(|error| format!("Не удалось проверить обновления: {error}"))?;
    Ok(match update {
        Some(update) => json!({
            "available": true,
            "currentVersion": current_version,
            "version": update.version,
            "notes": update.body.unwrap_or_default().chars().take(1000).collect::<String>(),
        }),
        None => json!({
            "available": false,
            "currentVersion": current_version,
        }),
    })
}
#[tauri::command]
async fn vpn_install_update(
    app: tauri::AppHandle,
    service: State<'_, Service>,
    updating: State<'_, Updating>,
) -> Result<(), String> {
    let _guard = begin_update(&updating)?;
    let backend = service.0.clone()?;
    let exit_app = app.clone();
    let updater = app
        .updater_builder()
        .on_before_exit(move || {
            let _ = backend.shutdown();
            exit_app.cleanup_before_exit();
        })
        .build()
        .map_err(|error| format!("Не удалось запустить установщик обновления: {error}"))?;
    let update = updater
        .check()
        .await
        .map_err(|error| format!("Не удалось проверить обновление перед установкой: {error}"))?
        .ok_or_else(|| "Новая версия больше недоступна".to_string())?;
    let progress_app = app.clone();
    let finished_app = app.clone();
    let mut downloaded = 0usize;
    update
        .download_and_install(
            move |chunk, total| {
                downloaded = downloaded.saturating_add(chunk);
                let _ = progress_app.emit(
                    "vpn:update-progress",
                    json!({"downloaded": downloaded, "total": total}),
                );
            },
            move || {
                let _ = finished_app.emit("vpn:update-progress", json!({"finished": true}));
            },
        )
        .await
        .map_err(|error| format!("Не удалось установить обновление: {error}"))
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
            let executable = std::env::current_exe()
                .map_err(|_| "Не удалось определить путь ShadowVPN".to_string())?;
            let command = format!("\"{}\"", executable.display());
            std::process::Command::new("schtasks.exe")
                .args([
                    "/Create",
                    "/TN",
                    AUTOSTART_TASK_NAME,
                    "/SC",
                    "ONLOGON",
                    "/TR",
                    &command,
                    "/RL",
                    "HIGHEST",
                    "/F",
                ])
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

fn show_main_window(app: &tauri::AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

fn request_shutdown(app: tauri::AppHandle, restart: bool) {
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
                if restart {
                    app.restart();
                } else {
                    app.exit(0);
                }
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

fn request_exit(app: tauri::AppHandle) {
    request_shutdown(app, false);
}

fn request_restart(app: tauri::AppHandle) {
    request_shutdown(app, true);
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _, _| {
            show_main_window(app);
        }))
        .plugin(tauri_plugin_updater::Builder::new().build())
        .manage(Closing::default())
        .manage(Updating::default())
        .setup(|app| {
            let open_item =
                MenuItem::with_id(app, "tray-open", "Открыть ShadowVPN", true, None::<&str>)?;
            let connect_item =
                MenuItem::with_id(app, "tray-connect", "Включить VPN", true, None::<&str>)?;
            let disconnect_item =
                MenuItem::with_id(app, "tray-disconnect", "Выключить VPN", false, None::<&str>)?;
            let restart_item =
                MenuItem::with_id(app, "tray-restart", "Перезапустить", true, None::<&str>)?;
            let quit_item = MenuItem::with_id(app, "tray-quit", "Выйти", true, None::<&str>)?;
            let first_separator = PredefinedMenuItem::separator(app)?;
            let second_separator = PredefinedMenuItem::separator(app)?;
            let tray_menu = Menu::with_items(
                app,
                &[
                    &open_item,
                    &first_separator,
                    &connect_item,
                    &disconnect_item,
                    &second_separator,
                    &restart_item,
                    &quit_item,
                ],
            )?;
            let mut tray_builder = TrayIconBuilder::new()
                .tooltip("ShadowVPN")
                .menu(&tray_menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id().as_ref() {
                    "tray-open" => show_main_window(app),
                    "tray-connect" => {
                        let _ = app.emit("vpn:tray-action", "connect");
                    }
                    "tray-disconnect" => {
                        let _ = app.emit("vpn:tray-action", "disconnect");
                    }
                    "tray-restart" => request_restart(app.clone()),
                    "tray-quit" => request_exit(app.clone()),
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        show_main_window(tray.app_handle());
                    }
                });
            if let Some(icon) = app.default_window_icon() {
                tray_builder = tray_builder.icon(icon.clone());
            }
            tray_builder.build(app)?;

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
            let traffic_handle = app.handle().clone();
            let log_handle = app.handle().clone();
            let connect_menu_item = connect_item.clone();
            let disconnect_menu_item = disconnect_item.clone();
            let service = Backend::spawn(
                &directory.join(name),
                move |state| {
                    let _ = connect_menu_item.set_enabled(state == "disconnected");
                    let _ = disconnect_menu_item.set_enabled(state == "connected");
                    let _ = state_handle.emit("vpn:state", state);
                },
                move |profile| {
                    let _ = profile_handle.emit("vpn:profile", profile);
                },
                move |traffic| {
                    let _ = traffic_handle.emit("vpn:traffic", traffic);
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
            vpn_import_many,
            vpn_open_url,
            vpn_connect,
            vpn_disconnect,
            vpn_ping,
            vpn_public_ip,
            vpn_device_info,
            vpn_check_update,
            vpn_install_update,
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
