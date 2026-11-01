# JDWeb HTTP API

Contract between the JDWeb backend and the web frontend. It is binding for both sides.

All paths are below `/api/v1`, except `/healthz`.

## Conventions

- Errors have the shape `{"error":{"code":"...","message":"..."}}`.
- Error codes: `unauthorized`, `totp_required`, `forbidden`, `bad_request`, `not_found`, `conflict`, `rate_limited`, `jd_offline`, `jd_error`, `internal`.
- State-changing requests need the header `X-JDWeb-Request: 1` and a matching `Origin`, otherwise the response is 403.
- IDs are always strings in JSON.

## Public

| Route | Body | Response |
|---|---|---|
| `GET /setup/status` | | `{required}` |
| `POST /setup` | `{token,username,password}` | |
| `POST /login` | `{username,password,totp?}` | |
| `GET /healthz` | | `ok` |

`totp` in `POST /login` also accepts recovery codes.

## Account

| Route | Body | Response |
|---|---|---|
| `POST /logout` | | |
| `GET /session` | | `{username,totpEnabled,version}` |
| `GET /sessions` | | |
| `DELETE /sessions` | | signs out all other sessions |
| `POST /password` | `{current,new}` | |
| `POST /totp/enroll` | | `{secret,uri,qrSvg}` |
| `POST /totp/confirm` | `{code}` | `{recoveryCodes}` |
| `POST /totp/disable` | `{password}` | |

## State

- `GET /status` returns `Status`.
- `GET /downloads` returns `{packages:[DlPackage],links:[DlLink]}`.
- `GET /linkgrabber` returns `{packages:[LgPackage],links:[LgLink]}`.

Both lists are in JDownloader order and gzip-compressed.

### `Status`

```json
{"connection":"connected|syncing|offline|misconfigured","connectionDetail":"","controller":"IDLE|RUNNING|PAUSE|STOPPING","speed":0,
 "downloads":{"packages":0,"links":0,"bytesTotal":0,"bytesLoaded":0,"running":0,"eta":null},
 "linkgrabber":{"packages":0,"links":0,"bytesTotal":0,"crawling":false},
 "captchas":0,"dialogs":0,
 "toggles":{"limit":false,"limitBytes":0,"reconnect":false,"premium":false,"clipboard":false,"stopAfter":false},
 "jd":{"revision":50639,"docker":true,"url":"http://..."}}
```

### Objects

- `DlPackage`: `id,name,hosts[],childCount,bytesTotal,bytesLoaded,speed,eta,enabled,finished,running,saveTo,status,priority,comment,downloadPassword`.
- `DlLink`: `id,packageId,name,host,url,bytesTotal,bytesLoaded,speed,eta,enabled,finished,running,skipped,skipReason,extractionStatus,status,priority,comment,downloadPassword,addedDate,finishedDate,state`.
- `LgPackage`: `id,name,hosts[],childCount,bytesTotal,saveTo,onlineCount,offlineCount,unknownCount,tempUnknownCount,enabled,priority,comment,downloadPassword`.
- `LgLink`: `id,packageId,name,host,url,bytesTotal,availability,enabled,priority,variants,variant,comment,downloadPassword`.

`state` is derived from the other fields. It is the first match of this list:

1. `disabled`: `enabled` is false.
2. `extractError`: `extractionStatus` starts with `ERROR`.
3. `finished`: `finished` is true.
4. `skipped`: `skipped` is true.
5. `running`: `running` is true.
6. `waiting`: none of the above.

`eta` is `null` when unknown or negative.

Fields missing in JDownloader responses mean `false`, `0` or empty.

## Server-Sent Events

`GET /events`. Events:

| Event | Payload | Notes |
|---|---|---|
| `status` | `Status` | |
| `patch` | `{list,links:{id:{fields}},packages:{id:{fields}}}` | batched every 500 ms |
| `structure` | `{list}` | the client reloads the list |
| `captcha` | `{count}` | |
| `dialog` | `{count}` | |
| `connection` | `{connection,detail}` | |

A keepalive comment is sent every 20 s.

## Control

- `POST /controller/{start,stop,pause,resume}`
- `POST /speedlimit` with `{enabled,bytesPerSecond}`
- `POST /toggles/{premium,clipboard,reconnect,stop-after}`
- `POST /reconnect`

## Actions

`POST /downloads/actions/{action}` and `POST /linkgrabber/actions/{action}`.

The base body is `{linkIds:[],packageIds:[]}`.

| action | additional fields | downloads | linkgrabber |
|---|---|---|---|
| `enable`, `disable`, `remove`, `check-online`, `split-by-hoster` | | yes | yes |
| `priority` | `priority` | yes | yes |
| `rename-link` | `linkId,name` | yes | yes |
| `rename-package` | `packageId,name` | yes | yes |
| `directory` | `directory` | yes | yes |
| `password` | `password` | yes | yes |
| `new-package` | `name,directory` | yes | yes |
| `move-links` | `afterLinkId,destPackageId` | yes | yes |
| `move-packages` | `afterPackageId` | yes | yes |
| `cleanup` | `action,mode,selection` | yes | yes |
| `urls` returns `{links:[{id,url}]}` | | yes | yes |
| `force`, `resume`, `reset`, `unskip`, `stopmark-set`, `stopmark-remove` | | yes | no |
| `to-downloads` | `start` (bool) | no | yes |
| `abort`, `clear`, `add-variant` (`linkId,variantId`), `set-variant` (`linkId,variantId`) | | no | yes |

Additional routes:

- `GET /downloads/stopmark`
- `POST /linkgrabber/add` with `{links,packageName,destinationFolder,extractPassword,downloadPassword,autostart,autoExtract,deepDecrypt,overwritePackagizerRules,priority}`
- `POST /linkgrabber/container` with `{name,contentBase64}`
- `GET /linkgrabber/variants/{linkId}`
- `GET /linkgrabber/folders` returns `{folders:[{path,free,size}]}`

## Captchas

- `GET /captchas`
- `GET /captchas/{id}` returns `{id,hoster,type,kind:"image|interactive",imageDataUrl?,explain?,timeout,created}`
- `POST /captchas/{id}/solve` with `{result}`
- `POST /captchas/{id}/skip` with `{type}`

## Dialogs

- `GET /dialogs`
- `GET /dialogs/{id}`
- `POST /dialogs/{id}/answer` with `{data}`

## Extraction

- `GET /extraction/queue`
- `POST /extraction/start` with `{linkIds,packageIds}`
- `POST /extraction/cancel` with `{id}`
- `POST /extraction/archives` with `{linkIds,packageIds}` returns a list
- `PUT /extraction/archives/{id}`
- `POST /extraction/passwords` with `{password}`

## Accounts

- `GET /accounts`
- `POST /accounts` with `{hoster,username,password}`
- `PUT /accounts/{id}` with `{username,password}`
- `POST /accounts/actions/{enable,disable,remove,refresh}` with `{ids}`
- `GET /accounts/hosters`
- `GET /basicauth`, `POST /basicauth`
- `PUT /basicauth/{id}`
- `POST /basicauth/remove` with `{ids}`

## Settings

- `GET /config?q=&interface=&extensions=`: values with a secret key are masked and flagged `secret:true`.
- `POST /config/reveal` with `{interface,storage,key}`
- `PUT /config` with `{interface,storage,key,value}`
- `POST /config/reset`
- `GET /plugins?q=`
- `PUT /plugins`
- `POST /plugins/reset`
- `GET /extensions`
- `POST /extensions/{install,enable,disable}` with `{id}`

## System

- `GET /system`
- `POST /system/{restart,exit}`
- `POST /system/{shutdown,hibernate,standby}`: 403 when `docker:true`.
- `GET /update`
- `POST /update/check`
- `POST /update/apply`
- `GET /diagnostics`
- `POST /diagnostics/acknowledge`
- `GET /connection`, `PUT /connection` with `{url}`

## Icons

- `GET /icons/hoster/{name}`
- `GET /icons/file/{name}`

Both are proxied with an in-memory cache and a `Cache-Control` header.

## UI preferences

`GET /prefs` and `PUT /prefs`: free-form JSON up to 64 KB.
