// docker_test.go 镜像引用解析的单元测试（无需安装 docker 即可验证改写规则）。
package main

import "testing"

// TestParseDockerImage 验证各种镜像写法的解析：官方镜像、用户镜像、tag/digest、非 Hub registry。
func TestParseDockerImage(t *testing.T) {
	cases := []struct {
		in, path, suffix string
		hub              bool
	}{
		{"nginx", "library/nginx", "", true},
		{"nginx:1.27", "library/nginx", ":1.27", true},
		{"nginx@sha256:abc", "library/nginx", "@sha256:abc", true},
		{"nginx:1.27@sha256:abc", "library/nginx", ":1.27@sha256:abc", true},
		{"user/img", "user/img", "", true},
		{"user/img:v1", "user/img", ":v1", true},
		{"docker.io/nginx", "library/nginx", "", true},
		{"docker.io/user/img:tag", "user/img", ":tag", true},
		{"ghcr.io/o/r:tag", "ghcr.io/o/r", ":tag", false},
		{"registry:5000/img", "registry:5000/img", "", false},
		{"localhost/img", "localhost/img", "", false},
	}
	for _, c := range cases {
		r := parseDockerImage(c.in)
		if r.path != c.path || r.suffix != c.suffix || r.hub != c.hub {
			t.Errorf("parseDockerImage(%q) = {%q %q %v}, want {%q %q %v}",
				c.in, r.path, r.suffix, r.hub, c.path, c.suffix, c.hub)
		}
	}
}

// TestRewriteDockerImage 验证模板改写：{image} 替换为 path+suffix。
func TestRewriteDockerImage(t *testing.T) {
	m := Mirror{DockerTemplate: "docker.1ms.run/{image}"}
	r := parseDockerImage("nginx:1.27")
	if got := rewriteDockerImage(m, r); got != "docker.1ms.run/library/nginx:1.27" {
		t.Errorf("rewriteDockerImage = %q", got)
	}
}
