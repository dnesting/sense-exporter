# sense-exporter

Sense-exporter is a Prometheus exporter for Sense Energy Monitor power usage data.

The Sense API that this tool relies on is unofficial and unsupported by Sense.
This repo has no affiliation with Sense.
This code could stop working at any time.

## Exported Variables

Device-specific metrics are tagged with `device_id`, `make`, `model`, `monitor`, `name`, `type`:

- `sense_device_watts` is the current power consumption of a Sense-detected or -integrated device
- `sense_device_active` describes whether a Sense-integrated device is currently active (0 or 1)
- `sense_device_online` describes whether a Sense-integrated device is currently online (0 or 1)

Sense-detected devices are the devices Sense has discovered by analyzing its power usage.
Sense-integrated devices are devices Sense has identified through an integration with a service
such as Hue.  I'm just making these terms up, but that's what appears to be the case.

Monitor-specific devices are tagged with `monitor`:

- `sense_monitor_hz` is the current mains frequency measured by the monitor
- `sense_monitor_up` is 1 while the exporter is able to collect data from a monitor
- `sense_monitor_volts` is the current voltage measured at each of the monitor's leads (tagged with `channel`)
- `sense_monitor_watts` is the current total power consumption measured by the monitor (`sense_device_watts` should sum to this number)
- `sense_scrape_time_seconds` is how long it took for the exporter to collect these metrics for the monitor

## Usage

```
sense-exporter --sense-config=config.yaml
sense-exporter --sense-email=you@example.com --password=s3cr3t
```

Configuration is described more fully below.

## Configuration

Sense-exporter can be configured with a YAML configuration file, command-line flags,
or environment variables.
If a config file is used, all other configuration is ignored.

### Configuration File

Usage: `sense-exporter -sense-config <filename>`

The config file is structured like this:

```yaml
accounts:
- credentials:
    email: you@example.com
    password: secret
```

Multiple accounts can be configured in this way.
The full list of options available under `credentials`:

- `email` defines a literal e-mail address
- `email-from` reads the e-mail address from the given file
- `password`
- `password-from`
- `mfa-from` reads the MFA code from the given file (for accounts that require MFA)
- `mfa-command` executes the given command and expects an MFA code as its output

### Flags

If no config file is provided, the following flags can be used, which operate exactly
as the YAML properties described above:

- `-sense-email`
- `-sense-email-from`
- `-sense-password`
- `-sense-password-from`
- `-sense-mfa-from`
- `-sense-mfa-command`

### Environment Variables

If no config file is provided, these environment variables will be used for any fields
not set by a flag:

- `SENSE_EMAIL`
- `SENSE_EMAIL_FROM`
- `SENSE_PASSWORD`
- `SENSE_PASSWORD_FROM`
- `SENSE_MFA_FROM`
- `SENSE_MFA_COMMAND`