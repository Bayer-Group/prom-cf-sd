# Prometheus CloudFoundry Service Discovery 

## Summary
This project provides prometheus service discovery for applications deployed on Cloud Foundry via the prometehus generic file sd mechanism.  It is designed to run in paralel with the prometheus processes/container, to call the Cloud Foundry API at configurable intervals and generate a list of Cloud Foundry application targets in Prometheus json format for the Prometheus File SD mechanism to ingest.

## Setup 

The service discovery mechanism requires a UAA clientid/secret that is authorized to access the Cloud Foundry API to list applictions, org, and spaces. To add a user `cfapi-readonly` with global_auditor permissions, edit your cf manifest file:

```
properties:
  uaa:
    clients:
      cfapi-readonly:
        authorities: cloud_controller.global_auditor
        authorized-grant-types: client_credentials,refresh_token
        scope: uaa.none
        secret: supersecret 
    scim:
      groups:
        cloud_controller.global_auditor: Readonly to all except secrets
```

or use the uaac client to create the user:

```
uaac target https://<YOUR UAA URL> --skip-ssl-validation
uaac token client get <YOUR ADMIN CLIENT ID> -s <YOUR ADMIN CLIENT SECRET>
uaac client add cfapi-readonly \
  --name cfapi-readonly \
  --secret supersecret \
  --authorized_grant_types client_credentials,refresh_token \
  --authorities cloud_controller.global_auditor
```

## Configuration

Any of the configuration parameters can be overloaded by using environment variables. The following
parameters are supported

| Environment variable          | Description            |
|-------------------------------|------------------------|
| API_ADDRESS                 | URL for the cloudfoundry API (Ex. https://api.cf.company.com) |
| CF_CLIENT_ID                | UUA ClientID  with sufficent API level access  |
| CF_CLIENT_SECRET            | Secret for the UAA ClientSecret |
| SKIP_SSL                    | Disable SSL validation if needed (defaults to false) |
| FREQUENCY                   | Frequency in minutes to refresh the application target list (defaults to 3) |
| OUTPUT_FILE                 | Location on the filesystem to write the generated target list to (defaults to /tmp/cf_targets.json) |

## Running

Because this project will generate a targets file which must be read by the Prometheus process, it is recommended to run this as a container on the same host as your prometheus container and have the two share a host volume.

To build the docker container, clone this repo locally and then run:
```
cd prom-cf-sd
docker build . -t prom-cf-sd:latest
```

Then run the container and write the target file to a host volume:
```
docker run -d -p 8080:8080 -e API_ADDRESS=https://api.cf.company.com -e CF_CLIENT_ID=cfapi-readonly -e CF_CLIENT_SECRET=password -e FREQUENCY=5 -e OUTPUT_FILE=/data/cf_targets.json -v /sd_data:/data prom-cf-sd:latest
```

Update the prometheus prometheus.yml to use the file based SD mechanism:
```
scrape_configs:
#  target_groups:
- job_name: cloudfoundry
  file_sd_configs:
    - files:
      - /sd_data/cf_targets.json
```

Tell prometheus to mount the host volumes where the target list gets written to and the new prometheus configuration file
```
docker run -p 9090:9090 -v /sd_data/:/sd_data/ -v /tmp/prometheus.yml:/etc/prometheus/prometheus.yml prom/prometheus
```

## Why 

Prometheus already has several built-in service discovery mechanims for use with widely adopted platforms.  Although Cloud Foundry would be considered a widely adopted platform, it seems the consensus from the community is that utilizing the generic file based sd mechanism is the favored approach for doing SD going forward.  This allows for a cleaner ecosystem where components like platform specific SD can be maintained and updated independently of the core code-base.



