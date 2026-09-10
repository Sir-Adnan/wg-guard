package web

import (
	"net/http"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// operationalForm retains only explicitly listed form fields. Secrets never
// enter Values, including after parsing or persistence failures.
type operationalForm struct {
	Values      map[string]string
	Fields      map[string]string
	Error       string
	SecretReset bool
}

func (f operationalForm) V(name string) string     { return f.Values[name] }
func (f operationalForm) Invalid(name string) bool { return f.Fields[name] != "" }

func submittedOperationalForm(r *http.Request, defaults map[string]string) operationalForm {
	f := operationalForm{Values: defaults, Fields: map[string]string{}}
	for key := range defaults {
		f.Values[key] = r.PostFormValue(key)
	}
	return f
}

func (s *Server) operationalFormStatus(w http.ResponseWriter, r *http.Request, f *operationalForm, err error) {
	status := http.StatusUnprocessableEntity
	f.Error = s.t(r, "forms.save_failed")
	if domain.CodeOf(err) == domain.CodeInternal {
		status = http.StatusInternalServerError
		f.Error = s.t(r, "common.error_generic")
		// Error values from a database/driver may contain submitted data.
		s.logError(r, "operational form save failed", nil)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
}

// Service errors have stable codes but no field metadata. Match only known
// fixed prefixes to associate feedback; never display the raw error message.
func operationalErrorField(err error, plan bool) string {
	message := strings.ToLower(err.Error())
	if plan {
		switch {
		case strings.HasPrefix(message, "plan name"):
			return "name"
		case strings.HasPrefix(message, "traffic limit"):
			return "traffic_limit_value"
		case strings.HasPrefix(message, "duration"):
			return "duration_value"
		}
		return ""
	}
	switch domain.CodeOf(err) {
	case domain.CodeInterfaceNameTaken:
		return "name"
	case domain.CodePortInUse:
		return "listen_port"
	case domain.CodeSubnetInvalid, domain.CodeSubnetOverlap:
		return "subnet"
	}
	for _, pair := range [][2]string{
		{"interface name", "name"}, {"interface index", "name"}, {"listen port", "listen_port"},
		{"mtu", "mtu"}, {"endpoint override", "endpoint_override"},
		{"headerprotectionkey", "obf_hpk"}, {"s1–s4", "obf_hpk"},
		{"jc", "obf_jc"}, {"jmin", "obf_jmin"}, {"jmax", "obf_jmax"},
		{"s1", "obf_s1"}, {"s2", "obf_s2"}, {"s3/s4", "obf_s3"},
		{"h1", "obf_h1"}, {"generated profile", "obf_enabled"},
	} {
		if strings.HasPrefix(message, pair[0]) {
			return pair[1]
		}
	}
	for _, key := range []string{"jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4", "padding", "rekey_after", "rekey_timeout", "reject_after", "keepalive", "max_handshake"} {
		if strings.HasPrefix(message, "obf_"+key+" ") {
			return "obf_" + key
		}
	}
	for _, key := range []string{"i1", "i2", "i3", "i4", "i5"} {
		if strings.HasPrefix(message, key+" ") {
			return "obf_" + key
		}
	}
	return ""
}

func operationalReturnPath(r *http.Request, list, readScope string) string {
	a := adminFrom(r)
	if a != nil && auth.Authorized(a.Role, a.Permissions, readScope) {
		return list
	}
	return "/"
}
