{{- define "hub.labels" -}}
app.kubernetes.io/name: cluster-comparator
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "hub.selectorLabels" -}}
app.kubernetes.io/name: cluster-comparator
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "hub.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}

{{- define "hub.tlsSecret" -}}{{ .Values.serviceAccount.name }}-tls{{- end -}}
{{- define "hub.sessionSecret" -}}{{ .Values.serviceAccount.name }}-oauth-session{{- end -}}
