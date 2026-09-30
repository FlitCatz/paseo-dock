# Paseo SBX Go 전환 설계

Paseo SBX의 실행 도구를 Go로 구성한다. 사용자가 요청한 언어 변경을 수행하면서 기존 명령과 여섯 sandbox provider의 동작을 유지한다. 이 문서는 사용자가 승인한 구현 기준이다. 구현 결과와 검증 범위는 `docs/implementation-log.md`의 Go 전환 기록에 정리했다.

## 전환 범위

- `internal/cli/`: build, load, create, register, list, shell, stop 명령을 Go로 옮긴다.
- `internal/launcher/`: Codex와 Claude 실행 인수, stdio 전달, MCP 설정 변환, 작업 경로 검증과 종료 신호 처리를 Go로 옮긴다.
- `internal/gateway/`: 세션별 HTTP MCP relay와 요청 및 응답 필터를 Go로 옮긴다.
- `internal/nodeinstall/`: lock 파일의 Node 버전과 SHA256을 사용하는 설치 도구를 Go로 대체한다.
- 기존 진입점과 테스트를 교체하고 Dockerfile, Docker 빌드 context, GitHub Actions와 README를 갱신한다.
- 기존 Node 기반 에이전트와 plugin installer, shell 설치 스크립트는 필요한 실행 도구로 유지한다.

## 구현 구조

Go 모듈을 만들고 `cmd/paseo-sbx`, `cmd/agent-wrapper`, `cmd/install-node`에 진입점을 둔다. CLI, launcher, relay의 로직은 `internal` 아래 목적별 패키지로 분리한다. HTTP, JSON, 암호학적 난수, SHA256과 프로세스 실행은 Go 표준 라이브러리를 사용한다. Node의 xz archive 해제는 이미지의 tar 도구로 수행하며 다운로드한 archive의 checksum을 먼저 검증한다.

호스트에는 빌드된 `paseo-sbx` 바이너리를 사용한다. launcher는 기존과 같이 workspace 밖의 `~/.local/share/paseo-sbx/<workspace-id>/agent-wrapper`에 설치한다. 설치된 launcher에는 별도 언어 실행 환경이 필요하지 않다. Docker는 별도 Go 빌드 단계에서 대상 Linux 아키텍처용 Node 설치 도구를 빌드한다.

## 호환성과 데이터

여섯 profile 이름, CLI의 기존 인수와 기본값, 이미지 이름, workspace 경로의 SHA256 앞 10자리로 만드는 runtime 식별자, registry JSON 구조를 유지한다. 이미 생성된 sandbox와 사용자 세션은 재사용한다. 기존 lock 파일의 에이전트, plugin, Node와 base image 고정값을 유지한다.

`register --apply`는 기존 Paseo 설정을 백업하고 여섯 provider의 실행 명령을 Go launcher로 갱신한다. 다른 provider 설정은 보존한다. 이번 구현과 테스트 단계에서는 실제 사용자 Paseo 설정이나 설치된 runtime을 자동 변경하지 않는다. 갱신에 사용할 명령과 결과를 문서화한다.

## 실행 및 보안 경계

인수는 shell 문자열로 재평가하지 않고 프로세스 인수 배열로 전달한다. launcher의 stdout은 에이전트 프로토콜 전용으로 유지하고 오류는 stderr로 출력한다. Codex의 JSON 메시지를 한 줄씩 처리하며 허용된 workspace 밖의 cwd와 symlink를 통한 경로 이탈을 거부한다. Claude의 MCP 설정은 기존 inline JSON 및 파일 입력을 지원한다. 종료 신호는 자식 프로세스 그룹에 전달하고 종료 시 relay와 임시 네트워크 규칙을 정리한다.

relay는 호스트 loopback에만 bind하고 세션별 임시 capability를 사용한다. upstream credential은 sandbox 설정이나 로그에 전달하지 않는다. 허용된 여덟 MCP 도구, sandbox provider 제한, caller와 생성한 자식 agent의 접근 범위, 설정 필터링, JSON 및 SSE 응답 필터링과 credential 제거를 유지한다. 잘못된 요청과 형식은 거부하고 원본 credential이 들어 있는 명령 인수를 오류에 출력하지 않는다.

## 검증과 완료 기준

기존 경계 테스트의 의미를 Go 테스트로 이식한다. 실제 HTTP 테스트 서버로 relay의 인증, 허용 및 거부 동작, JSON과 SSE 필터링, credential 제거를 확인한다. 가짜 sbx, docker, paseo 실행 파일로 CLI 인수와 launcher의 프로토콜 전달을 검증하며 실제 sandbox 생성이나 모델 호출 없이 테스트한다. 임시 디렉터리로 registry 호환성, 설정 병합과 백업, workspace 경로 제한을 검증한다.

`go test ./...`, `go test -race ./...`, `go vet ./...`를 실행하고 macOS와 Linux의 arm64 및 amd64 바이너리를 교차 빌드한다. Docker 빌드 단계의 검증은 Docker 사용 가능 여부와 실행 결과를 별도로 보고한다. 실제 모델 인증과 end-to-end 호출을 수행하지 않았다면 성공했다고 보고하지 않는다.

완료 시 이전 구현의 소스와 실행 지시를 제거하고, Go 빌드 및 실행 명령과 기존 설치를 갱신하는 방법을 README에 제공한다. 문서에는 과거 구현 기록과 현재 실행 안내를 구분한다.
