use crate::{
    model::{domain_matches, MockRule, RecordingState},
    storage::Store,
};
use std::sync::{Arc, RwLock};
use tauri::{AppHandle, Emitter};

pub struct Core {
    pub store: Store,
    recording: RwLock<RecordingState>,
    app: RwLock<Option<AppHandle>>,
}

impl Core {
    pub fn new(store: Store) -> Arc<Self> {
        Arc::new(Self {
            store,
            recording: RwLock::new(RecordingState::default()),
            app: RwLock::new(None),
        })
    }
    pub fn attach_app(&self, app: AppHandle) {
        *self.app.write().unwrap() = Some(app);
    }
    pub fn recording(&self) -> RecordingState {
        self.recording.read().unwrap().clone()
    }
    pub fn set_recording(&self, value: RecordingState) {
        *self.recording.write().unwrap() = value;
        self.emit("recording");
    }
    pub fn mocks(&self) -> Vec<MockRule> {
        self.store.mocks().unwrap_or_default()
    }
    pub fn should_mitm(&self, host: &str) -> bool {
        let recording = self.recording();
        if recording.active && domain_matches(host, &recording.domain) {
            return true;
        }
        self.store
            .projects()
            .unwrap_or_default()
            .iter()
            .any(|project| domain_matches(host, &project.domain))
    }
    pub fn emit(&self, kind: &str) {
        if let Some(app) = self.app.read().unwrap().as_ref() {
            let _ = app.emit("data-changed", kind);
        }
    }
}
