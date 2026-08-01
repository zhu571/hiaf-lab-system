package system

import (
	"testing"
)

func TestParseComposePSSingleObject(t *testing.T) {
	data := []byte(`{"Name":"lab-server","Service":"server","State":"running","Health":"healthy"}`)
	cs, err := parseComposePS(data)
	if err != nil {
		t.Fatalf("parseComposePS: %v", err)
	}
	if len(cs) != 1 || cs[0].Service != "server" || cs[0].Health != "healthy" {
		t.Errorf("unexpected parse: %+v", cs)
	}
}

func TestParseComposePSArray(t *testing.T) {
	data := []byte(`[{"Name":"lab-server","Service":"server","State":"running","Health":"healthy"},{"Name":"lab-py-agent","Service":"py-agent","State":"exited","Health":""}]`)
	cs, err := parseComposePS(data)
	if err != nil {
		t.Fatalf("parseComposePS: %v", err)
	}
	if len(cs) != 2 || cs[1].State != "exited" {
		t.Errorf("unexpected parse: %+v", cs)
	}
}

func TestParseComposePSMissingFields(t *testing.T) {
	data := []byte(`{"Service":"ioc","State":"running"}`)
	cs, err := parseComposePS(data)
	if err != nil {
		t.Fatalf("parseComposePS: %v", err)
	}
	if got := containerHealth(cs[0]); got != "running" {
		t.Errorf("containerHealth = %q, want running (Health 缺失回落 State)", got)
	}
}

func TestParseComposePSBadJSON(t *testing.T) {
	if _, err := parseComposePS([]byte("not-json")); err == nil {
		t.Fatal("expected error for bad json")
	}
	if _, err := parseComposePS([]byte("")); err != nil {
		t.Fatalf("empty input should parse to nil, got %v", err)
	}
}

func TestContainerHealth(t *testing.T) {
	cases := []struct {
		in   composePSContainer
		want string
	}{
		{composePSContainer{State: "running", Health: "healthy"}, "healthy"},
		{composePSContainer{State: "running", Health: ""}, "running"},
		{composePSContainer{State: "exited", Health: "unhealthy"}, "unhealthy"},
		{composePSContainer{State: "created", Health: ""}, "created"},
	}
	for _, c := range cases {
		if got := containerHealth(c.in); got != c.want {
			t.Errorf("containerHealth(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseComposeServices(t *testing.T) {
	data := []byte(`["server","postgres","migrate"]`)
	svcs, err := parseComposeServices(data)
	if err != nil {
		t.Fatalf("parseComposeServices: %v", err)
	}
	if len(svcs) != 3 || svcs[0] != "server" {
		t.Errorf("unexpected services: %v", svcs)
	}
	if _, err := parseComposeServices([]byte("[]")); err != nil {
		t.Errorf("empty array should parse: %v", err)
	}
}

func TestParseComposeImages(t *testing.T) {
	data := []byte(`["deploy-server"]`)
	imgs, err := parseComposeImages(data)
	if err != nil {
		t.Fatalf("parseComposeImages: %v", err)
	}
	if len(imgs) != 1 || imgs[0] != "deploy-server" {
		t.Errorf("unexpected images: %v", imgs)
	}
	// null 解析为空（不报错），由调用方按空结果处理
	imgs, err = parseComposeImages([]byte("null"))
	if err != nil {
		t.Fatalf("null 应解析为空: %v", err)
	}
	if len(imgs) != 0 {
		t.Errorf("null 应解析为空数组, got %v", imgs)
	}
}

func TestParseComposeProjects(t *testing.T) {
	data := []byte(`[{"Name":"deploy","Status":"running","ConfigFiles":"/opt/hiaf-lab-system/deploy/docker-compose.yml"}]`)
	projects, err := parseComposeProjects(data)
	if err != nil {
		t.Fatalf("parseComposeProjects: %v", err)
	}
	if len(projects) != 1 || projects[0].Name != "deploy" {
		t.Errorf("unexpected projects: %+v", projects)
	}
}
