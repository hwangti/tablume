//! Saved connections in a JSON file in the user config directory. Passwords
//! go to the OS keychain and never into the file, unless no keychain is
//! available.
//!
//! The directory is separate from the Go app's ("Tablume"), so both builds
//! can be tried side by side without touching each other's settings.

use crate::types::{ConnConfig, SchemaFilter};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::sync::Mutex;

const SERVICE: &str = "Tablume Tauri";

#[derive(Serialize, Deserialize, Clone, Default)]
#[serde(rename_all = "camelCase")]
struct Record {
    #[serde(flatten)]
    conn: ConnConfig,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    fallback_password: String,
    #[serde(default, skip_serializing_if = "String::is_empty")]
    fallback_ssh_password: String,
}

pub struct Store {
    dir: PathBuf,
    list: Mutex<Vec<Record>>,
}

pub fn dir() -> PathBuf {
    dirs::config_dir().unwrap_or_else(std::env::temp_dir).join("Tablume Tauri")
}

fn ssh_key(id: &str) -> String {
    format!("{id}/ssh")
}

fn kc_get(account: &str) -> Option<String> {
    keyring::Entry::new(SERVICE, account).ok()?.get_password().ok()
}

fn kc_set(account: &str, pw: &str) -> bool {
    keyring::Entry::new(SERVICE, account).and_then(|e| e.set_password(pw)).is_ok()
}

fn kc_delete(account: &str) {
    if let Ok(e) = keyring::Entry::new(SERVICE, account) {
        let _ = e.delete_credential();
    }
}

pub fn new_id() -> String {
    let mut b = [0u8; 8];
    let _ = getrandom::fill(&mut b);
    b.iter().map(|x| format!("{x:02x}")).collect()
}

impl Store {
    pub fn open() -> Result<Store, String> {
        let dir = dir();
        std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;
        let s = Store { dir, list: Mutex::new(vec![]) };
        match std::fs::read(s.path()) {
            Ok(b) => *s.list.lock().unwrap() = serde_json::from_slice(&b).map_err(|e| e.to_string())?,
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {}
            Err(e) => return Err(e.to_string()),
        }
        Ok(s)
    }

    fn path(&self) -> PathBuf {
        self.dir.join("connections.json")
    }

    fn save(&self, list: &[Record]) -> Result<(), String> {
        let b = serde_json::to_vec_pretty(list).map_err(|e| e.to_string())?;
        let tmp = self.path().with_extension("json.tmp");
        std::fs::write(&tmp, b).map_err(|e| e.to_string())?;
        std::fs::rename(&tmp, self.path()).map_err(|e| e.to_string())
    }

    /// Saved connections without passwords.
    pub fn list(&self) -> Vec<ConnConfig> {
        self.list
            .lock()
            .unwrap()
            .iter()
            .map(|r| {
                let mut c = r.conn.clone();
                c.password.clear();
                c.ssh_password.clear();
                c
            })
            .collect()
    }

    /// A connection including its stored password.
    pub fn get(&self, id: &str) -> Option<ConnConfig> {
        let list = self.list.lock().unwrap();
        let r = list.iter().find(|r| r.conn.id == id)?;
        let mut c = r.conn.clone();
        if c.save_password {
            c.password = kc_get(id).unwrap_or_else(|| r.fallback_password.clone());
        }
        if c.ssh {
            c.ssh_password = kc_get(&ssh_key(id)).unwrap_or_else(|| r.fallback_ssh_password.clone());
        }
        Some(c)
    }

    /// Inserts or updates a connection. An empty password on an existing
    /// connection keeps the stored one.
    pub fn save_conn(&self, mut c: ConnConfig) -> Result<ConnConfig, String> {
        let mut list = self.list.lock().unwrap();
        if c.id.is_empty() {
            c.id = new_id();
        }
        let idx = list.iter().position(|r| r.conn.id == c.id);
        let mut rec = Record { conn: c.clone(), ..Default::default() };
        rec.conn.password.clear();
        rec.conn.ssh_password.clear();
        if let Some(i) = idx {
            rec.fallback_password = list[i].fallback_password.clone();
            rec.fallback_ssh_password = list[i].fallback_ssh_password.clone();
        }
        if !c.ssh {
            kc_delete(&ssh_key(&c.id));
            rec.fallback_ssh_password.clear();
        } else if !c.ssh_password.is_empty() {
            rec.fallback_ssh_password = if kc_set(&ssh_key(&c.id), &c.ssh_password) { String::new() } else { c.ssh_password.clone() };
        }
        if !c.save_password {
            kc_delete(&c.id);
            rec.fallback_password.clear();
        } else if !c.password.is_empty() {
            rec.fallback_password = if kc_set(&c.id, &c.password) { String::new() } else { c.password.clone() };
        }
        let out = rec.conn.clone();
        match idx {
            Some(i) => list[i] = rec,
            None => list.push(rec),
        }
        self.save(&list)?;
        Ok(out)
    }

    pub fn delete(&self, id: &str) -> Result<(), String> {
        let mut list = self.list.lock().unwrap();
        if let Some(i) = list.iter().position(|r| r.conn.id == id) {
            list.remove(i);
            kc_delete(id);
            kc_delete(&ssh_key(id));
            return self.save(&list);
        }
        Ok(())
    }

    pub fn set_schema_filter(&self, id: &str, f: Option<SchemaFilter>) -> Result<(), String> {
        let mut list = self.list.lock().unwrap();
        if let Some(r) = list.iter_mut().find(|r| r.conn.id == id) {
            r.conn.schemas = f;
            return self.save(&list);
        }
        Ok(())
    }

    /// Puts the connections in the order of ids; ones not listed keep their
    /// relative order at the end.
    pub fn reorder(&self, ids: &[String]) -> Result<(), String> {
        let mut list = self.list.lock().unwrap();
        let rank = |r: &Record| ids.iter().position(|x| *x == r.conn.id).unwrap_or(ids.len());
        list.sort_by_key(rank); // stable
        self.save(&list)
    }
}
