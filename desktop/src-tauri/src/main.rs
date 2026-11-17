#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
mod connection;
use std::sync::atomic::{AtomicU64, Ordering};
use tauri::{Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder};
use tauri::menu::{Menu, MenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::webview::NewWindowResponse;
use tauri_plugin_opener::OpenerExt;
use connection::Connection;

static NEXT_MEDIA_WINDOW: AtomicU64 = AtomicU64::new(1);

fn show_connection(app: &tauri::AppHandle) -> Result<(), String> {
    if let Some(window) = app.get_webview_window("connection") {
        window.show().and_then(|_| window.set_focus()).map_err(|_| "无法显示连接窗口".to_owned())?;
        return Ok(());
    }
    WebviewWindowBuilder::new(app, "connection", WebviewUrl::App("connection.html".into()))
        .title("连接绮梦影库").inner_size(490.0, 470.0).min_inner_size(380.0, 400.0).resizable(true)
        .build().map_err(|_| "无法创建连接窗口".to_owned())?;
    Ok(())
}

fn open_library(app: &tauri::AppHandle, connection: &Connection) -> Result<(), String> {
    let origin = connection::normalize(&connection.origin)?;
    if let Some(window) = app.get_webview_window("library") {
        // Rebuilding installs a guard for the new origin; a guard must not keep accepting the old server.
        window.destroy().map_err(|_| "无法关闭旧连接窗口")?;
    }
    let allowed = origin.clone();
    let new_window_origin = origin.clone();
    let handle = app.clone();
    WebviewWindowBuilder::new(app, "library", WebviewUrl::External(origin))
        .title("绮梦影库 Next").inner_size(1280.0, 840.0).min_inner_size(720.0, 540.0)
        .decorations(true).disable_drag_drop_handler()
        .on_navigation(move |url| connection::same_origin(&allowed, url))
        .on_new_window(move |url, features| {
            if connection::same_origin(&new_window_origin, &url) {
                let allowed = new_window_origin.clone();
                let label = format!("media-{}", NEXT_MEDIA_WINDOW.fetch_add(1, Ordering::Relaxed));
                match WebviewWindowBuilder::new(&handle, label, WebviewUrl::External(url)).window_features(features)
                    .title("绮梦原件").on_navigation(move |target| connection::same_origin(&allowed, target)).build() {
                    Ok(window) => NewWindowResponse::Create { window },
                    Err(_) => { eprintln!("Unable to open original media window"); NewWindowResponse::Deny },
                }
            } else {
                if matches!(url.scheme(), "http" | "https") && url.username().is_empty() && url.password().is_none() {
                    if handle.opener().open_url(url.as_str(), None::<&str>).is_err() { eprintln!("Unable to open external browser"); }
                }
                NewWindowResponse::Deny
            }
        })
        .build().map_err(|_| "无法创建媒体窗口，请检查 WebView2 是否可用".to_owned())?;
    Ok(())
}

#[tauri::command]
fn current_connection(app: tauri::AppHandle, window: WebviewWindow) -> Result<Option<Connection>, String> {
    if window.label() != "connection" { return Err("此窗口无配置权限".into()); }
    let directory = app.path().app_config_dir().map_err(|_| "无法定位配置目录")?;
    connection::load(&directory)
}

#[tauri::command]
fn save_connection(app: tauri::AppHandle, window: WebviewWindow, address: String) -> Result<Connection, String> {
    if window.label() != "connection" { return Err("此窗口无配置权限".into()); }
    let url = connection::normalize(&address)?;
    let connection = Connection { origin: url.to_string() };
    let directory = app.path().app_config_dir().map_err(|_| "无法定位配置目录")?;
    connection::save(&directory, &connection)?;
    open_library(&app, &connection)?;
    window.hide().map_err(|_| "已连接，但连接窗口未能隐藏")?;
    Ok(connection)
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![current_connection, save_connection])
        .setup(|app| {
            let open = MenuItem::with_id(app, "open", "打开影库", true, None::<&str>)?;
            let setup = MenuItem::with_id(app, "setup", "切换服务器地址", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&open, &setup, &quit])?;
            let mut tray = TrayIconBuilder::new().menu(&menu).show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "quit" => app.exit(0),
                    "setup" => { if show_connection(app).is_err() { eprintln!("Unable to show connection window"); } },
                    "open" => { if let Some(window) = app.get_webview_window("library") { if window.show().and_then(|_| window.set_focus()).is_err() { eprintln!("Unable to focus library"); } } else if show_connection(app).is_err() { eprintln!("Unable to show connection window"); } },
                    _ => {},
                }).on_tray_icon_event(|tray, event| {
                    if matches!(event, TrayIconEvent::Click { button: MouseButton::Left, button_state: MouseButtonState::Up, .. }) {
                        if let Some(window) = tray.app_handle().get_webview_window("library") { if window.show().and_then(|_| window.set_focus()).is_err() { eprintln!("Unable to focus library"); } }
                    }
                });
            if let Some(icon) = app.default_window_icon() { tray = tray.icon(icon.clone()); }
            tray.build(app)?;
            let directory = app.path().app_config_dir()?;
            if std::env::args().any(|argument| argument == "--setup") { show_connection(app.handle())?; }
            else { match connection::load(&directory) { Ok(Some(connection)) => open_library(app.handle(), &connection)?, _ => show_connection(app.handle())? } }
            Ok(())
        })
        .on_window_event(|window, event| {
            if window.label() == "library" && matches!(event, tauri::WindowEvent::CloseRequested { .. }) { window.app_handle().exit(0); }
        })
        .run(tauri::generate_context!())
        .expect("Unable to initialize the desktop connection shell");
}
