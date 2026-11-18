#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]
mod connection;
use std::sync::{atomic::{AtomicU64, Ordering}, Mutex};
use tauri::{Manager, WebviewUrl, WebviewWindow, WebviewWindowBuilder};
use tauri::ipc::CapabilityBuilder;
use tauri::menu::{Menu, MenuItem};
use tauri::tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent};
use tauri::webview::NewWindowResponse;
use tauri_plugin_opener::OpenerExt;
use connection::Connection;

static NEXT_WINDOW: AtomicU64 = AtomicU64::new(1);
static ACTIVE_LIBRARY: AtomicU64 = AtomicU64::new(0);
static SWITCHING: Mutex<()> = Mutex::new(());

fn active_label() -> String {
    format!("library-{}", ACTIVE_LIBRARY.load(Ordering::Acquire))
}

fn show_connection(app: &tauri::AppHandle) -> Result<(), String> {
    if let Some(window) = app.get_webview_window("connection") {
        window.unminimize().and_then(|_| window.show()).and_then(|_| window.set_focus()).map_err(|_| "无法显示连接窗口".to_owned())?;
        return Ok(());
    }
    WebviewWindowBuilder::new(app, "connection", WebviewUrl::App("connection.html".into()))
        .title("连接服务器 · 绮梦影库").inner_size(420.0, 340.0).resizable(false)
        .on_navigation(connection::is_setup_page)
        .on_new_window(|_, _| NewWindowResponse::Deny)
        .build().map_err(|_| "无法创建连接窗口".to_owned())?;
    Ok(())
}

fn open_library(app: &tauri::AppHandle, connection: &Connection, save: bool) -> Result<(), String> {
    let _switch = SWITCHING.try_lock().map_err(|_| "已有连接正在打开，请稍后再试")?;
    let origin = connection::normalize(&connection.origin)?;
    let generation = NEXT_WINDOW.fetch_add(1, Ordering::Relaxed);
    let label = format!("library-{generation}");
    let serialized_origin = origin.origin().ascii_serialization();
    app.add_capability(CapabilityBuilder::new(format!("own-titlebar-{generation}"))
        .local(false).webview(label.clone()).remote(format!("{serialized_origin}/*")).permission("own-titlebar"))
        .map_err(|_| "无法为所选地址建立窗口操作权限")?;

    let allowed = origin.clone();
    let popup_origin = origin.clone();
    let handle = app.clone();
    let script = format!("if (window.top === window && location.origin === {}) {{ {} }}",
        serde_json::to_string(&serialized_origin).map_err(|_| "服务器地址格式无效")?, include_str!("titlebar.js"));
    let window = WebviewWindowBuilder::new(app, label, WebviewUrl::External(origin))
        .title("绮梦影库").inner_size(1200.0, 800.0).min_inner_size(900.0, 600.0)
        .visible(false).decorations(false).disable_drag_drop_handler().initialization_script(script)
        .on_navigation(move |url| connection::same_origin(&allowed, url))
        .on_new_window(move |url, features| {
            if ACTIVE_LIBRARY.load(Ordering::Acquire) != generation { return NewWindowResponse::Deny; }
            if connection::same_origin(&popup_origin, &url) {
                let allowed = popup_origin.clone();
                let label = format!("media-{generation}-{}", NEXT_WINDOW.fetch_add(1, Ordering::Relaxed));
                // WebView2 supplies the requested URL after it adopts the new view.
                let blank = tauri::Url::parse("about:blank").expect("valid blank page URL");
                match WebviewWindowBuilder::new(&handle, label, WebviewUrl::External(blank)).window_features(features)
                    .title("绮梦原件").on_navigation(move |target| {
                        ACTIVE_LIBRARY.load(Ordering::Acquire) == generation &&
                            (target.as_str() == "about:blank" || connection::same_origin(&allowed, target))
                    })
                    .on_new_window(|_, _| NewWindowResponse::Deny).build() {
                    Ok(window) => NewWindowResponse::Create { window },
                    Err(_) => { eprintln!("Unable to open original media window"); NewWindowResponse::Deny },
                }
            } else {
                if connection::external_web_url(&url) && handle.opener().open_url(url.as_str(), None::<&str>).is_err() { eprintln!("Unable to open external browser"); }
                NewWindowResponse::Deny
            }
        })
        .build().map_err(|_| "无法创建媒体窗口，请检查 WebView2 是否可用".to_owned())?;
    let activate = || -> Result<(), String> {
        window.show().and_then(|_| window.set_focus()).map_err(|_| "无法显示媒体窗口")?;
        if save {
            let directory = app.path().app_config_dir().map_err(|_| "无法定位配置目录")?;
            connection::save(&directory, connection)?;
        }
        Ok(())
    };
    if let Err(error) = activate() {
        if window.destroy().is_err() { eprintln!("Unable to dispose failed connection"); }
        return Err(error);
    }
    let previous = ACTIVE_LIBRARY.swap(generation, Ordering::AcqRel);
    let previous_label = format!("library-{previous}");
    let child_prefix = format!("media-{previous}-");
    for (label, old) in app.webview_windows() {
        if (label == previous_label || label.starts_with(&child_prefix)) && old.destroy().is_err() {
            eprintln!("Unable to close a previous connection window");
        }
    }
    Ok(())
}

#[derive(serde::Deserialize)]
#[serde(rename_all = "camelCase")]
enum WindowAction { Minimize, ToggleMaximize, Close, Drag }

#[tauri::command]
async fn window_action(window: WebviewWindow, action: WindowAction) -> Result<(), String> {
    if window.label() != active_label() { return Err("此窗口不能操作主窗口".into()); }
    match action {
        WindowAction::Minimize => window.minimize(),
        WindowAction::ToggleMaximize => window.is_maximized().and_then(|maximized| if maximized { window.unmaximize() } else { window.maximize() }),
        WindowAction::Close => window.close(),
        WindowAction::Drag => window.start_dragging(),
    }.map_err(|_| "窗口操作未完成".to_owned())
}

fn check_local_window(window: &WebviewWindow) -> Result<(), String> {
    if window.label() == "connection" && window.url().is_ok_and(|url| connection::is_setup_page(&url)) { Ok(()) }
    else { Err("此窗口无配置权限".into()) }
}

#[tauri::command]
async fn current_connection(app: tauri::AppHandle, window: WebviewWindow) -> Result<Option<Connection>, String> {
    check_local_window(&window)?;
    let directory = app.path().app_config_dir().map_err(|_| "无法定位配置目录")?;
    connection::load(&directory)
}

#[tauri::command]
async fn save_connection(app: tauri::AppHandle, window: WebviewWindow, address: String) -> Result<Connection, String> {
    check_local_window(&window)?;
    let connection = Connection { origin: connection::normalize(&address)?.to_string() };
    open_library(&app, &connection, true)?;
    window.hide().map_err(|_| "已连接，但连接窗口未能隐藏")?;
    Ok(connection)
}

fn reopen(app: tauri::AppHandle, settings: bool) {
    // Creating a WebView2 inside an IPC/menu callback can block the UI thread.
    tauri::async_runtime::spawn_blocking(move || {
        if !settings {
            if let Some(window) = app.get_webview_window(&active_label()) {
                if window.unminimize().and_then(|_| window.show()).and_then(|_| window.set_focus()).is_err() { eprintln!("Unable to focus library"); }
                return;
            }
        }
        if show_connection(&app).is_err() { eprintln!("Unable to show connection window"); }
    });
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![current_connection, save_connection, window_action])
        .setup(|app| {
            let open = MenuItem::with_id(app, "open", "打开主窗口", true, None::<&str>)?;
            let setup = MenuItem::with_id(app, "setup", "切换服务器地址", true, None::<&str>)?;
            let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&open, &setup, &quit])?;
            let mut tray = TrayIconBuilder::new().menu(&menu).show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "quit" => app.exit(0), "setup" => reopen(app.clone(), true), "open" => reopen(app.clone(), false), _ => {},
                }).on_tray_icon_event(|tray, event| {
                    if matches!(event, TrayIconEvent::Click { button: MouseButton::Left, button_state: MouseButtonState::Up, .. }) { reopen(tray.app_handle().clone(), false); }
                });
            if let Some(icon) = app.default_window_icon() { tray = tray.icon(icon.clone()); }
            tray.build(app)?;
            let directory = app.path().app_config_dir()?;
            if std::env::args().any(|argument| argument == "--setup") { show_connection(app.handle())?; }
            else { match connection::load(&directory) {
                Ok(Some(connection)) => if open_library(app.handle(), &connection, false).is_err() { show_connection(app.handle())?; },
                _ => show_connection(app.handle())?,
            } }
            Ok(())
        })
        .on_window_event(|window, event| {
            if matches!(event, tauri::WindowEvent::CloseRequested { .. }) && window.label() == active_label() { window.app_handle().exit(0); }
        })
        .run(tauri::generate_context!())
        .expect("Unable to initialize the desktop connection shell");
}
