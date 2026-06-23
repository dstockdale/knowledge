package knowledge

import "testing"

func TestContextHintsForFrontend(t *testing.T) {
	cases := []struct {
		name  string
		task  string
		paths []string
		want  bool
	}{
		{name: "assets js path", paths: []string{"assets/js/admin/media.tsx"}, want: true},
		{name: "assets css path", paths: []string{"assets/css/app.css"}, want: true},
		{name: "priv static path", paths: []string{"priv/static/app.js"}, want: true},
		{name: "phoenix web path", paths: []string{"lib/boopbup_web/live/admin_live.ex"}, want: true},
		{name: "react layout terms", task: "build React admin layout", want: true},
		{name: "tailwind term", task: "tighten Tailwind spacing", want: true},
		{name: "css term", task: "adjust CSS for the app shell", want: true},
		{name: "heex term", task: "update HEEx component markup", want: true},
		{name: "component term", task: "extract reusable component", want: true},
		{name: "admin alone", task: "improve admin ingestion workflow", want: false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			hints := contextHintsFor(tt.task, tt.paths, nil)
			if hints["frontend"] != tt.want {
				t.Fatalf("frontend hint = %v, want %v", hints["frontend"], tt.want)
			}
		})
	}
}
