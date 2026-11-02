package testjd

// Like JDownloader the fake leaves out false, zero and empty values.

const etaUnknown = -2147483648

func put(m map[string]any, key string, v any) {
	switch x := v.(type) {
	case bool:
		if !x {
			return
		}
	case int:
		if x == 0 {
			return
		}
	case int64:
		if x == 0 {
			return
		}
	case string:
		if x == "" {
			return
		}
	case []string:
		if len(x) == 0 {
			return
		}
	}
	m[key] = v
}

func (s *Server) renderLink(l List, lk *Link, q query) map[string]any {
	m := map[string]any{"uuid": lk.UUID, "packageUUID": lk.PackageUUID, "name": lk.Name}
	f := q.flags
	if f["addedDate"] {
		put(m, "addedDate", lk.AddedDate)
	}
	if f["bytesTotal"] {
		put(m, "bytesTotal", lk.BytesTotal)
	}
	if f["comment"] {
		put(m, "comment", lk.Comment)
	}
	if f["enabled"] {
		put(m, "enabled", !lk.Disabled)
	}
	if f["host"] {
		put(m, "host", lk.Host)
	}
	if f["password"] {
		put(m, "downloadPassword", lk.Password)
	}
	if f["priority"] {
		put(m, "priority", lk.Priority)
	}
	if f["url"] {
		put(m, "url", lk.URL)
	}
	if f["status"] {
		put(m, "status", linkStatus(lk))
	}
	if f["advancedStatus"] {
		m["advancedStatus"] = advancedStatus(lk)
	}
	if l == Linkgrabber {
		if f["availability"] {
			a := lk.Availability
			if a == "" {
				a = "ONLINE"
			}
			m["availability"] = a
		}
		if len(lk.Variants) > 1 {
			if f["variants"] {
				m["variants"] = true
			}
			if f["variantID"] || f["variantName"] || f["variantIcon"] {
				cur := lk.Variants[0]
				for _, v := range lk.Variants {
					if v.ID == lk.VariantID {
						cur = v
					}
				}
				m["variant"] = cur
			}
		}
		return m
	}
	if f["bytesLoaded"] {
		put(m, "bytesLoaded", lk.BytesLoaded)
	}
	if f["eta"] {
		put(m, "eta", lk.ETA)
	}
	if f["extractionStatus"] {
		put(m, "extractionStatus", lk.ExtractionStatus)
	}
	if f["finished"] {
		put(m, "finished", lk.Finished)
	}
	if f["finishedDate"] {
		put(m, "finishedDate", lk.FinishedDate)
	}
	if f["running"] {
		put(m, "running", lk.Running)
	}
	if f["skipped"] {
		put(m, "skipped", lk.Skipped)
	}
	if f["speed"] {
		put(m, "speed", lk.Speed)
	}
	return m
}

func linkStatus(lk *Link) string {
	switch {
	case lk.Status != "":
		return lk.Status
	case lk.Skipped && lk.SkipReason != "":
		return "Skipped - " + lk.SkipReason
	case lk.Finished:
		return "Finished"
	case lk.Running:
		return "Download"
	}
	return ""
}

func advancedStatus(lk *Link) map[string]any {
	m := map[string]any{}
	avail := map[string]any{"iconKey": "true", "label": "Online", "id": "TRUE"}
	if lk.Availability == "OFFLINE" {
		avail = map[string]any{"iconKey": "error", "label": "Offline", "id": "FALSE"}
	}
	m["AvailableStatus"] = avail
	if lk.SkipReason != "" {
		m["SkipReason"] = map[string]any{"label": "Skipped - " + lk.SkipReason, "id": lk.SkipReason}
	}
	if lk.Finished {
		m["FinalLinkState"] = map[string]any{"iconKey": "true", "label": "Finished", "id": "FINISHED"}
	}
	if lk.ExtractionStatus != "" {
		m["ExtractionStatus"] = map[string]any{"label": lk.ExtractionStatus, "id": lk.ExtractionStatus}
	}
	if lk.Running {
		m["PluginProgress"] = map[string]any{"current": lk.BytesLoaded, "total": lk.BytesTotal, "id": "DOWNLOAD", "label": "Download"}
	}
	return m
}

func (s *Server) renderPackage(l List, p *pkg, q query) map[string]any {
	m := map[string]any{"uuid": p.UUID, "name": p.Name}
	f := q.flags
	sum := p.sums()
	if f["bytesTotal"] {
		put(m, "bytesTotal", sum.total)
	}
	if f["childCount"] {
		put(m, "childCount", len(p.links))
	}
	if f["comment"] {
		put(m, "comment", p.Comment)
	}
	if f["enabled"] {
		put(m, "enabled", !p.Disabled)
	}
	if f["hosts"] {
		put(m, "hosts", sum.hosts)
	}
	if f["priority"] {
		put(m, "priority", p.Priority)
	}
	if f["saveTo"] {
		put(m, "saveTo", p.SaveTo)
	}
	if l == Linkgrabber {
		if f["availableOnlineCount"] {
			put(m, "onlineCount", sum.online)
		}
		if f["availableOfflineCount"] {
			put(m, "offlineCount", sum.offline)
		}
		if f["availableUnknownCount"] {
			put(m, "unknownCount", sum.unknown)
		}
		if f["availableTempUnknownCount"] {
			put(m, "tempUnknownCount", sum.tempUnknown)
		}
		return m
	}
	if f["bytesLoaded"] {
		put(m, "bytesLoaded", sum.loaded)
	}
	if f["eta"] && sum.running {
		if sum.speed > 0 && sum.total > sum.loaded {
			m["eta"] = (sum.total - sum.loaded) / sum.speed
		} else {
			m["eta"] = int64(etaUnknown)
		}
	}
	if f["finished"] {
		put(m, "finished", sum.finished)
	}
	if f["running"] {
		put(m, "running", sum.running)
	}
	if f["speed"] {
		put(m, "speed", sum.speed)
	}
	return m
}
