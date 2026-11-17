use serde::{Deserialize, Serialize};
use std::{fs, io, path::Path};
use url::Url;

#[derive(Clone, Serialize, Deserialize)]
pub struct Connection { pub origin: String }

pub fn normalize(input: &str) -> Result<Url, String> {
    let trimmed = input.trim();
    if trimmed.is_empty() { return Err("请输入服务器地址".into()); }
    let value = if trimmed.contains("://") { trimmed.to_owned() } else { format!("http://{trimmed}") };
    let url = Url::parse(&value).map_err(|_| "服务器地址格式不正确")?;
    if !matches!(url.scheme(), "http" | "https") || url.host_str().is_none() { return Err("只接受 HTTP 或 HTTPS 服务器地址".into()); }
    if !url.username().is_empty() || url.password().is_some() || url.query().is_some() || url.fragment().is_some() || url.path() != "/" {
        return Err("请填写服务器根地址，不要包含账户、密码、路径、参数或片段".into());
    }
    Ok(url)
}

pub fn same_origin(expected: &Url, requested: &Url) -> bool {
    matches!(requested.scheme(), "http" | "https") && requested.username().is_empty() && requested.password().is_none() && expected.origin() == requested.origin()
}

pub fn load(directory: &Path) -> Result<Option<Connection>, String> {
    let destination = directory.join("connection.json");
    let previous = directory.join("connection.previous.json");
    let path = if destination.exists() { destination } else { previous };
    match fs::read(path) {
        Ok(bytes) => {
            let connection: Connection = serde_json::from_slice(&bytes).map_err(|_| "已保存的连接配置损坏，请重新设置")?;
            normalize(&connection.origin)?;
            Ok(Some(connection))
        },
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(None),
        Err(_) => Err("无法读取连接配置，请检查应用配置目录权限".into()),
    }
}

pub fn save(directory: &Path, connection: &Connection) -> Result<(), String> {
    fs::create_dir_all(directory).map_err(|_| "无法创建应用配置目录")?;
    let destination = directory.join("connection.json");
    let temporary = directory.join("connection.pending.json");
    let previous = directory.join("connection.previous.json");
    let bytes = serde_json::to_vec(connection).map_err(|_| "无法序列化连接配置")?;
    fs::write(&temporary, bytes).map_err(|_| "无法写入连接配置")?;
    if destination.exists() {
        if previous.exists() { fs::remove_file(&previous).map_err(|_| "无法更新旧连接配置")?; }
        fs::rename(&destination, &previous).map_err(|_| "无法保护旧连接配置")?;
    }
    fs::rename(&temporary, &destination).map_err(|_| "连接配置提交失败；旧连接仍保留")?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn bare_address_and_ipv6_are_normalized() {
        assert_eq!(normalize(" example.test:8450 ").unwrap().as_str(), "http://example.test:8450/");
        assert_eq!(normalize("https://[2001:db8::1]/").unwrap().as_str(), "https://[2001:db8::1]/");
    }
    #[test]
    fn secrets_and_non_web_protocols_are_rejected() {
        for value in ["file:///tmp", "javascript:alert(1)", "https://a:b@example.test", "https://example.test/?token=x", "https://example.test/app", "https://example.test/#x"] { assert!(normalize(value).is_err(), "{value}"); }
    }
    #[test]
    fn navigation_requires_the_full_origin() {
        let expected = normalize("https://example.test:8450").unwrap();
        assert!(same_origin(&expected, &Url::parse("https://example.test:8450/album?q=x").unwrap()));
        for value in ["http://example.test:8450/", "https://example.test/", "https://example.test.evil.test:8450/", "file:///tmp"] { assert!(!same_origin(&expected, &Url::parse(value).unwrap())); }
    }
}
