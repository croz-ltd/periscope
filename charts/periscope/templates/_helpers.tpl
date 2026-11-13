{{/*
periscope.host is the host name of the hub route: the host value when set,
otherwise the name OpenShift gives the route by default
(<route>-<namespace>.<cluster apps domain>), with the domain read from the
cluster Ingress config. It is empty when neither is known, for example when the
chart is rendered with `helm template` and no host value, as Argo CD does.
*/}}
{{- define "periscope.host" -}}
{{- if .Values.host -}}
{{- .Values.host -}}
{{- else -}}
{{- $ingress := lookup "config.openshift.io/v1" "Ingress" "" "cluster" -}}
{{- if and $ingress $ingress.spec $ingress.spec.domain -}}
{{- printf "periscope-periscope.%s" $ingress.spec.domain -}}
{{- end -}}
{{- end -}}
{{- end -}}
