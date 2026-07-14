{{- define "join.labels" -}}
app.kubernetes.io/name: periscope-join
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
