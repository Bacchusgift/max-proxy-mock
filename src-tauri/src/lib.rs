mod certificate;
mod commands;
mod core;
mod model;
mod pac;
mod proxy;
mod storage;
mod system_proxy;

use core::Core;
use std::{fs, path::PathBuf, sync::Arc};
use tauri::Manager;

pub fn run() {
    tracing_subscriber::fmt().with_target(false).try_init().ok();
    tauri::Builder::default()
        .setup(|app| {
            let data_dir = app.path().app_data_dir()?;
            fs::create_dir_all(&data_dir)?;
            let store = storage::Store::open(&data_dir.join("max-proxy-mock.db"))?;
            for legacy in legacy_database_candidates() {
                if store.import_legacy_if_empty(&legacy).unwrap_or(false) {
                    tracing::info!(path = %legacy.display(), "Imported legacy Go database");
                    break;
                }
            }
            let core = Core::new(store);
            core.attach_app(app.handle().clone());
            app.manage(Arc::clone(&core));
            let certificates = data_dir.join("certificates");
            tauri::async_runtime::spawn({
                let core = Arc::clone(&core);
                async move {
                    if let Err(error) = proxy::start(core, certificates).await {
                        tracing::error!("Rust proxy failed: {error}");
                    }
                }
            });
            tauri::async_runtime::spawn(async move {
                if let Err(error) = pac::start(core).await {
                    tracing::error!("PAC server failed: {error}");
                }
            });
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            commands::api_call,
            commands::reveal_certificate,
            commands::certificate_status,
            commands::install_certificate
        ])
        .run(tauri::generate_context!())
        .expect("failed to run Max Proxy Mock desktop app");
}

fn legacy_database_candidates() -> Vec<PathBuf> {
    let mut candidates = Vec::new();
    if let Ok(path) = std::env::var("MAX_PROXY_LEGACY_DATA") {
        candidates.push(PathBuf::from(path));
    }
    if let Ok(current) = std::env::current_dir() {
        candidates.push(current.join("data/max-proxy-mock.db"));
    }
    candidates.push(PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../data/max-proxy-mock.db"));
    candidates
}
