# JDWeb HTTP API

Contract between the JDWeb backend and the web frontend. It is binding for both sides.

All paths are below `/api/v1`, except `/healthz`.

## Conventions

- Errors have the shape `{"error":{"code":"...","message":"..."}}`. The message is English text meant for people. It never repeats what JDownloader said (stacktraces, input), so the frontend shows its own translation of the code and may show the message below it.
- Error codes and their HTTP status:

  | Code | Status | Meaning |
  |---|---|---|
  | `unauthorized` | 401 | No session, or wrong credentials. Only this code means "sign in again". |
  | `totp_required` | 401 | The login needs the two-factor code (`totp`). |
  | `forbidden` | 403 | Missing header or foreign `Origin`, a wrong current password (`/password`, `/totp/*`), or an action that is not offered (system actions in Docker). |
  | `bad_request` | 400, 413 | Invalid input. Nothing was sent to JDownloader. 413 when the body is larger than allowed. |
  | `not_found` | 404 | Unknown route or action. |
  | `conflict` | 409 | State does not allow it: setup already done, two-factor already on, JDownloader wants another dialog answered first. |
  | `rate_limited` | 429 | Too many attempts. The header `Retry-After` has the seconds to wait. |
  | `jd_offline` | 503 | JDownloader does not answer, or no address is set. |
  | `jd_error` | 502, 404 | JDownloader answered with an error, or another service answers at the address. 404 when a captcha, a dialog or an extraction is gone (solved, answered or finished meanwhile). Actions on links and packages that are gone are no error, see Actions. |
  | `internal` | 500, 503 | Bug, full disk, a database call that took too long (500), or a request cancelled by the browser (503). Details are in the log only. |

- State-changing requests (every method but GET and HEAD, public routes included) need the header `X-JDWeb-Request: 1` and an `Origin` that names the host the request went to, otherwise the response is 403. The check comes after the session check: without a session the answer is 401.
- No GET route changes anything. GET routes only read.
- IDs are always strings of digits in JSON, in requests and responses. A number, a negative number or zero is a 400. Where a request names the place "behind" which something goes (`afterLinkId`, `afterPackageId`), an empty string or `"-1"` means the top of the list.
- A request that succeeds and has nothing to say answers `204 No Content`. System actions and `POST /update/apply` answer `202`.
- JSON bodies are limited to 1 MiB, `/linkgrabber/add` to 8 MiB, `/linkgrabber/container` to 64 MiB, `PUT /prefs` to 64 KiB. These two routes also get more time to upload and to answer than the 30 s and 60 s of other routes.
- Unknown fields in request bodies are ignored.
- The server answers every request with `Content-Security-Policy`, `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. API answers carry `Cache-Control: no-store`, except icons.

## Public

| Route | Body | Response |
|---|---|---|
| `GET /setup/status` | | `{required}` |
| `POST /setup` | `{token,username,password}` | 204 |
| `POST /login` | `{username,password,totp?}` | 204 and the session cookie |
| `GET /healthz` | | `ok` (text) |

`totp` in `POST /login` also accepts recovery codes. A login without the code for an account with two-factor is `401 totp_required` and does not count as a failed attempt.

`required` is true before the account exists, and for one hour after `jdweb reset-password`. In the second case `POST /setup` takes the reset token as `token` and the new `password`; `username` is ignored, the account keeps its name, two-factor is switched off and all sessions end. `POST /setup` does not log in: the browser calls `POST /login` next.

A wrong or missing setup token is `400 bad_request`, setup after it was done is `409 conflict`, too many wrong tokens `429 rate_limited`.

The session cookie is `jdweb_session`: `HttpOnly`, `SameSite=Strict`, `Path=/`, `Secure` on HTTPS (behind a proxy only with `JDWEB_TRUSTED_PROXIES`).

## Account

| Route | Body | Response |
|---|---|---|
| `POST /logout` | | |
| `GET /session` | | `{username,totpEnabled,version}` |
| `GET /sessions` | | `[{id,createdAt,lastSeenAt,expiresAt,userAgent,current}]`, times as RFC 3339 |
| `DELETE /sessions` | | 204, signs out all other sessions |
| `POST /password` | `{current,new}` | 204, signs out the other sessions |
| `POST /totp/enroll` | `{password}` | `{secret,uri,qrSvg}` |
| `POST /totp/confirm` | `{code}` | `{recoveryCodes}`, ten single-use codes |
| `POST /totp/disable` | `{password}` | 204 |

`POST /logout` answers 204 and removes the cookie. A wrong password in `/password` and `/totp/*` is `403 forbidden`, not 401, so that the frontend does not take it for an expired session.

## State

- `GET /status` returns `Status`.
- `GET /downloads` returns `{packages:[DlPackage],links:[DlLink]}`.
- `GET /linkgrabber` returns `{packages:[LgPackage],links:[LgLink]}`.

Both lists are in JDownloader order. They are gzip-compressed when the request has `Accept-Encoding: gzip`. While JDownloader is offline they still answer with the last known state (stopped, speed 0); `connection` in `Status` says why.

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
- `DlLink`: `id,packageId,name,host,url,bytesTotal,bytesLoaded,speed,eta,enabled,finished,running,skipped,skipReason,extractionStatus,status,priority,comment,downloadPassword,addedDate,finishedDate,finalState,state`.
- `LgPackage`: `id,name,hosts[],childCount,bytesTotal,saveTo,onlineCount,offlineCount,unknownCount,tempUnknownCount,enabled,priority,comment,downloadPassword`.
- `LgLink`: `id,packageId,name,host,url,bytesTotal,availability,enabled,priority,variants,variant,comment,downloadPassword`.

`state` is derived from the other fields. It is the first match of this list:

1. `disabled`: `enabled` is false.
2. `extractError`: `extractionStatus` starts with `ERROR`.
3. `failed`: `finalState` is set and does not start with `FINISHED`.
4. `finished`: `finished` is true.
5. `skipped`: `skipped` is true.
6. `running`: `running` is true.
7. `waiting`: none of the above.

`finalState` is how JDownloader ended the download, the id of `advancedStatus.FinalLinkState` (`FINISHED`, `OFFLINE` for "File not found" while downloading, other failures as JDownloader names them), empty while it has not ended. It is the only machine readable verdict: the text of `status` is never evaluated. Recorded on a real JDownloader: network errors and HTTP 429/503 are no final state, they leave the link waiting with a retry (`ConditionalSkipReason`, `status` shows the reason); a refused or vanished file ends as `OFFLINE`. Events do not carry `finalState`: it arrives with the next reload of the list (JDownloader sends `REFRESH_CONTENT` when a link ends), and a link that starts running again loses it at once.

`eta` is `null` when unknown or negative. `controller` is never JDownloader's raw `STOPPED_STATE`: that and any unknown state are `IDLE`.

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

A stream belongs to the session that opened it. When that session ends (logout, `DELETE /sessions`, password change, reset, expiry) the stream is closed: at once for the first three, at the next keepalive for the others. The browser's reconnect then gets a 401. The stream starts with `retry: 3000`, then one `status` and one `connection` event with the current state. A keepalive comment (`: keepalive`) is sent every 20 s. Each event is flushed at once; the stream is never compressed. A browser that falls too far behind is disconnected: it reconnects (EventSource does this by itself) and must load the lists again. Open streams count as open browsers: JDWeb polls JDownloader faster while at least one is open. The server ends all streams when it shuts down.

## Control

- `POST /controller/{start,stop,pause,resume}`: 204.
- `POST /speedlimit` with `{enabled,bytesPerSecond}`: 204. Always bytes per second, 0 to 2147483647 (JDownloader keeps the limit in a 32-bit integer and refuses more, found against revision 50639). `bytesPerSecond` 0 only switches. Switching on with no limit ever set is a 400.
- `POST /toggles/{premium,clipboard,reconnect,stop-after}` with an optional `{enabled}`: answers `{enabled}` with the new state. JDownloader can only flip a switch. With `enabled` in the body a switch that has this state already is left alone, so a browser with an old view cannot flip it the wrong way. Switching premium off makes JDownloader ask for confirmation in a dialog and not answer: after 3 s the answer is `202 {"pending":true}` and the question shows up in `/dialogs`.
- `POST /reconnect`: 204.

## Actions

`POST /downloads/actions/{action}` and `POST /linkgrabber/actions/{action}`.

The base body is `{linkIds:[],packageIds:[]}`. An action that works on a selection needs at least one id (400 otherwise). Everything is validated before JDownloader is called: unknown enum values, missing fields and bad ids are a 400 and nothing is sent. An unknown action, or one the list does not have, is a 404 `not_found`.

Ids that JDownloader dropped meanwhile are no error: it answers such an action with success, so JDWeb does too (204) and then reloads the list, so that the browser converges (the reload arrives as `patch` and `structure` events).

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
| `force`, `resume`, `reset` | | yes | no |
| `unskip` | `reason` (optional) | yes | no |
| `stopmark-set` | exactly one of `linkId`, `packageId` | yes | no |
| `stopmark-remove` | | yes | no |
| `to-downloads` | `start` (bool) | no | yes |
| `abort`, `clear` | | no | yes |
| `set-variant` | `linkId,variantId` | no | yes |
| `add-variant` | `linkId,variantId`, optional `afterLinkId,destPackageId` | no | yes |

Enums, checked by JDWeb:

- `priority`: `HIGHEST`, `HIGHER`, `HIGH`, `DEFAULT`, `LOW`, `LOWER`, `LOWEST`.
- `cleanup`: `action` is one of `DELETE_ALL`, `DELETE_DISABLED`, `DELETE_FAILED`, `DELETE_FINISHED`, `DELETE_OFFLINE`, `DELETE_DUPE`, `DELETE_MODE`. `mode` is one of `REMOVE_LINKS_ONLY`, `REMOVE_LINKS_AND_RECYCLE_FILES`, `REMOVE_LINKS_AND_DELETE_FILES` and has no default: it decides what happens to files on disk, so a request without it is a 400. `selection` is one of `SELECTED`, `UNSELECTED`, `ALL`, `NONE`. Only `ALL` may go without ids (it ignores them). `SELECTED`, `UNSELECTED` and `NONE` need at least one id, otherwise 400: JDownloader reads an empty selection as "everything", and nobody should clean up everything by accident. To clean up everything, say `ALL`.
- `unskip` `reason`: empty (all) or one of the skip reasons of JDownloader.

`rename-*` names and `new-package` names must not be empty. `password` may be an empty string, which clears it, but must be present. `clear` empties the whole linkgrabber, `to-downloads` with `start:true` also starts the downloads. `add-variant` without `afterLinkId` and `destPackageId` puts the copy behind the original, in its package.

Additional routes:

- `GET /downloads/stopmark` returns `{marked:false}` or `{marked:true,linkId,packageId,name}`.
- `POST /linkgrabber/add` with `{links,packageName,destinationFolder,extractPassword,downloadPassword,autostart,autoExtract,deepDecrypt,overwritePackagizerRules,priority}` returns `{jobId}`. `links` is a list of at least one non-blank link; the booleans are optional and keep JDownloader's default when absent.
- `POST /linkgrabber/container` with `{name,contentBase64}` returns `{jobId}`. The extension of `name` (`dlc`, `ccf`, `rsdf`, ...) tells JDownloader the format. The file is at most 40 MiB.
- `GET /linkgrabber/variants/{linkId}` returns `{variants:[{id,name,iconKey}]}`.
- `GET /linkgrabber/folders` returns `{folders:[{path,free,size}]}`, the folders used recently, at most 20. `free` and `size` are bytes, 0 when JDownloader cannot read the folder.

## Captchas

- `GET /captchas` returns a list of `{id,hoster,type,kind,explain?,timeout,created,remaining}`, without pictures.
- `GET /captchas/{id}` returns `{id,hoster,type,kind:"image|interactive",imageDataUrl?,explain?,timeout,created,remaining}`. `imageDataUrl` is a `data:image/...` URL and only present for `image`. `kind` is `interactive` for challenges the browser cannot show as a picture (reCAPTCHA, hCaptcha, click and similar) and when JDownloader has no usable picture. Interactive captchas can be explained and skipped, not solved here. The classification by type name is a guess until real jobs are seen (Concept R7).
- `POST /captchas/{id}/solve` with `{result}`: 204.
- `POST /captchas/{id}/skip` with `{type}`: 204. `type` is one of `SINGLE`, `BLOCK_HOSTER`, `BLOCK_ALL_CAPTCHAS`, `BLOCK_PACKAGE`, `REFRESH`, `STOP_CURRENT_ACTION`, `TIMEOUT`.

A captcha that was solved or timed out meanwhile is `404 jd_error` with a message that says so.

## Dialogs

- `GET /dialogs` returns a list of `{id,type,properties}`. `type` is a Java interface name, `properties` a map of text for the browser to show as text.
- `GET /dialogs/{id}` returns the same plus `answer`, a map of the fields an answer may hold with their Java types.
- `POST /dialogs/{id}/answer` with `{data}`: 204. `data` is a flat object of texts, numbers and booleans, for example `{"closereason":"OK"}`. JDownloader asks its questions one after the other: answering another than the first is `409 conflict`. A dialog that is gone is `404 jd_error`.

## Extraction

- `GET /extraction/queue` returns a list of `{archiveId,archiveName,type,controllerId,controllerStatus,states}`. `controllerId` is a number (-1 without a running controller) and the argument of `cancel`.
- `POST /extraction/start` with `{linkIds,packageIds}` returns `{started:{controllerId:bool}}`.
- `POST /extraction/cancel` with `{id}` (the controller id as string): 204, or `404 jd_error` when it finished already.
- `POST /extraction/archives` with `{linkIds,packageIds}` returns a list of archives as in the queue, each with `settings` (`{archiveId,extractPath,finalPassword,passwords,autoExtract,removeDownloadLinksAfterExtraction,removeFilesAfterExtraction,secret}` or null). `finalPassword` and `passwords` are secrets, masked like in `/config`: `"••••••"` and `["••••••"]` when set, empty when not set, and `secret` is true.
- `POST /extraction/archives/{id}/reveal` returns `{finalPassword,passwords}` with the real values. Every call is logged with the archive id, never the values.
- `PUT /extraction/archives/{id}` with the fields of `settings`: 204. The `id` is the `archiveId`. Fields that are absent keep their value, `passwords` included; a given list replaces the stored one (an empty list clears it). The mask as `finalPassword` or inside `passwords` is a 400, as in `PUT /config`.
- `POST /extraction/passwords` with `{password}`: 204.

## Accounts

- `GET /accounts` returns a list of `{id,hoster,username,enabled,valid,errorType,errorString,trafficLeft,trafficMax,validUntil}`. The password is never returned. `trafficLeft`, `trafficMax` and `validUntil` (Unix milliseconds) are -1 when unknown.
- `POST /accounts` with `{hoster,username,password}`: 204. JDownloader checks the account in the background; `GET /accounts` shows the result.
- `PUT /accounts/{id}` with `{username,password}`: 204. Both are required.
- `POST /accounts/actions/{enable,disable,remove,refresh}` with `{ids}`: 204. `refresh` forces the check.
- `GET /accounts/hosters` returns a list of host names.
- `GET /basicauth` returns a list of `{id,type,hostMask,username,enabled,created,lastValidated}`, `type` is `HTTP` or `FTP`. `POST /basicauth` with `{type,hostMask,username,password}` returns `{id}`.
- `PUT /basicauth/{id}` with `{type,hostMask,username,password,enabled?}`: 204. The password is required again, because the stored one is unknown here.
- `POST /basicauth/remove` with `{ids}`: 204.
- Passwords of accounts and entries go to JDownloader only. They are never stored, never logged and never part of an error message.

## Settings

- `GET /config?q=&interface=&extensions=` returns `{entries:[{interface,storage,key,type,abstractType,docs,value,defaultValue,enumLabel?,enumOptions?,secret}]}`. `q` is a regular expression on interface and key (at most 256 characters), `interface` restricts to one config interface, `extensions=true` includes the extensions. The answer is gzip-compressed when accepted.
- Masking: an entry whose key matches `password|apikey|secret|token` (case-insensitive) and whose `abstractType` is `STRING` or `STRING_LIST` has `secret:true`, and `value` and `defaultValue` show `"••••••"` (six bullets, U+2022) for a text and `["••••••"]` for a list. An empty or null value stays as it is, so the browser can tell "not set" from "set". Numbers and switches are never masked, whatever the key.
- `POST /config/reveal` with `{interface,storage,key}` returns `{value}` with the real value. Only for keys that match the pattern, otherwise 400. Every reveal is logged with interface and key, never the value.
- `PUT /config` with `{interface,storage,key,value}`: 204. `storage` is empty for the general config. A value that is the mask, or a list that holds it, is a 400 and is not sent: the browser must send a new value or leave the setting alone. JDownloader refusing the value (wrong type, unknown key) is a 400 with a hint.
- `POST /config/reset` with `{interface,storage,key}`: 204.
- `GET /plugins?q=` returns `{plugins:[{displayName,className,pattern,version}]}`, filtered by `q` in name or class name (case-insensitive).
- `GET /plugins/settings?q=&interface=` returns `{entries:[...]}` like `/config` for the settings of plugins, with the same masking. `storage` is the display name of the plugin.
- `PUT /plugins` with `{interface,storage,key,value}` and `POST /plugins/reset` with `{interface,storage,key}`: 204. `storage` (the plugin) is required, masks are refused as in `PUT /config`.
- `POST /plugins/reveal` with `{interface,storage,key}` (`storage` is the display name of the plugin and required) returns `{value}` with the real value, like `POST /config/reveal` and with the same rules and logging. `/config/reveal` does not work for plugin settings: JDownloader's `config/get` does not know the storage of a plugin and answers an empty body, so the value comes from `plugins/query`.
- `GET /extensions` returns a list of `{id,name,description,enabled,installed,iconKey,configInterface}`.
- `POST /extensions/{install,enable,disable}` with `{id}`: 204, or `409 conflict` when JDownloader refuses.

## System

- `GET /system` returns `{version,revision,docker,url,headless,osFamily,osString,javaVersion,javaName,heapUsed,heapCommitted,heapMax,startedAt}`. `version` is JDWeb's, `revision` JDownloader's, `startedAt` Unix milliseconds.
- `POST /system/{restart,exit}`: 202. JDownloader is gone a moment later; the browser sees `connection` change.
- `POST /system/{shutdown,hibernate,standby}`: 403 `forbidden` when JDownloader reports `docker:true` (checked live at each call, JDownloader is not asked then), otherwise 202. `shutdown` takes an optional `{force}`.
- `GET /update` returns `{available}`.
- `POST /update/check`: 204.
- `POST /update/apply`: 202. JDownloader restarts to install the update.
- `GET /diagnostics` returns `{checks:[{id,status,messageKey,detail}],acknowledged}`. `status` is `ok`, `warn` or `fail`, `messageKey` selects the translated text of the frontend, `detail` has the values in plain English. `acknowledged` says that the user confirmed the warning about the API without a login.
- `POST /diagnostics/acknowledge` returns `{acknowledged:true}`. Changing the address with `PUT /connection` clears it again, a new address deserves a new look.
- `GET /connection` returns `{url,source,connection,detail}`. `source` is `ui` (saved in the web interface), `env` (`JDWEB_JD_URL`) or `default`. `PUT /connection` with `{url}` cleans the address (adds `http://`, lower-cases, removes trailing slashes), saves it, switches at once and answers like `GET`. If the switch fails, the saved address is taken back. A saved address wins over `JDWEB_JD_URL`. An address that cannot be one (empty, other scheme, no host, port out of range, user and password, query) is a 400 whose message says why. Whether JDownloader answers there is not checked here: `connection` and `detail` (`misconfigured` with a hint, or `offline`) tell.

## Icons

- `GET /icons/hoster/{name}`
- `GET /icons/file/{name}`

Both are proxied with an in-memory cache (at most 512 icons and 32 MiB, least recently used out) and `Cache-Control: private, max-age=86400` (the icons are behind the session). The answer is the image of JDownloader (PNG, 16x16) with its `Content-Type`. Only `image/png`, `image/jpeg`, `image/gif`, `image/webp` and `image/x-icon` are passed on, anything else is `502 jd_error`. A failed lookup of a name is remembered for 5 minutes, so a hostile name does not hit JDownloader again and again; an offline JDownloader is not remembered. An unknown host or file type gets a generic icon, not an error. Names longer than 255 characters or with control characters are a 400.

## UI preferences

`GET /prefs` returns the stored JSON object, `{}` when nothing was saved. `PUT /prefs` takes a JSON object up to 64 KiB and answers 204. Anything but an object is a 400.

## Web app

All other `GET` paths serve the web app and need no session (the login page must load). Files under `/assets/` are cached for a year, everything else must be revalidated. Paths with a segment that starts with a dot (`.vite/`, `.env`, `.gitkeep`) are always a 404. Content types are fixed for `.js`, `.css`, `.svg`, `.webmanifest`, `.woff2`, `.json`, `.png`, `.ico`. Files outside `/assets/` carry an `ETag` (hash of the content), so a revisit is a 304. A path without a file extension that is no file gets `index.html` (the app routes it); a missing file with an extension, or one under `/assets/`, is a 404 and never HTML. Unknown paths below `/api` are a JSON 404. A build without the web app answers with a plain text 404 that says so.
