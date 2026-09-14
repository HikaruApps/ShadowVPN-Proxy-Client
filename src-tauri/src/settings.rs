use std::{collections::HashSet, net::IpAddr};

pub fn validate_dns_request(
    dns: Option<String>,
    dns_servers: Option<Vec<String>>,
) -> Result<(String, Vec<String>), String> {
    let dns = dns.unwrap_or_else(|| "cloudflare".into());
    if ["cloudflare", "google", "quad9"].contains(&dns.as_str()) {
        return Ok((dns, Vec::new()));
    }
    if dns != "custom" {
        return Err("Выбран неизвестный DNS-сервер".into());
    }
    let values = dns_servers.unwrap_or_default();
    if values.is_empty() || values.len() > 4 {
        return Err("Укажите от одного до четырёх DNS IP-адресов".into());
    }
    let mut unique = HashSet::with_capacity(values.len());
    let mut servers = Vec::with_capacity(values.len());
    for value in values {
        if value.len() > 45 {
            return Err("Пользовательский DNS должен содержать только IPv4 или IPv6-адреса".into());
        }
        let address: IpAddr = value
            .trim()
            .parse()
            .map_err(|_| "Пользовательский DNS должен содержать только IPv4 или IPv6-адреса")?;
        let broadcast = matches!(address, IpAddr::V4(ip) if ip.octets() == [255, 255, 255, 255]);
        if address.is_unspecified() || address.is_multicast() || broadcast {
            return Err("Этот DNS IP-адрес нельзя использовать".into());
        }
        if unique.insert(address) {
            servers.push(address.to_string());
        }
    }
    Ok((dns, servers))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_built_in_and_custom_dns() {
        assert_eq!(
            validate_dns_request(None, None).unwrap(),
            ("cloudflare".into(), vec![])
        );
        assert_eq!(
            validate_dns_request(
                Some("custom".into()),
                Some(vec![" 192.168.1.1 ".into(), "192.168.1.1".into()])
            )
            .unwrap(),
            ("custom".into(), vec!["192.168.1.1".into()])
        );
    }

    #[test]
    fn rejects_invalid_custom_dns() {
        for values in [
            vec![],
            vec!["not-an-ip".into()],
            vec!["0.0.0.0".into()],
            vec!["::".into()],
            vec!["224.0.0.1".into()],
        ] {
            assert!(validate_dns_request(Some("custom".into()), Some(values)).is_err());
        }
    }
}
