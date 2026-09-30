# Paseo에서 선택하는 여섯 Docker Sandbox 템플릿

상태: 사용자가 기존 Paseo 연결과 자동 위임을 포함한 구현 진행을 승인함. 구현 진행 중.

## 목적과 확인된 요구사항

기존 호스트 Paseo 한 곳에서 Codex와 Claude Code의 여섯 독립 환경을 선택한다.
각 환경에는 Superpowers, GSD Core, Ponytail 중 하나를 설치한다. 같은 환경에
세 도구를 모두 설치하는 구성은 아니다. 설치 소스와 템플릿을 보관하여 새 머신이나
새 프로젝트에서 필요한 환경을 다시 만든다.

사용자는 2026-09-30에 기존 Paseo 한 곳에서 여섯 환경을 선택하는 연결 방식을 선택했다.
GitHub 업로드는 추후 작업이며 이번 단계에서는 업로드 가능한 로컬 산출물을 준비한다.

| Paseo provider / 기본 sandbox 이름 | 에이전트 | 설치 도구 |
| --- | --- | --- |
| codex-superpowers | Codex | Superpowers |
| codex-gsd-core | Codex | GSD Core |
| codex-ponytail | Codex | Ponytail |
| claude-superpowers | Claude Code | Superpowers |
| claude-gsd-core | Claude Code | GSD Core |
| claude-ponytail | Claude Code | Ponytail |

## 조사 결과

- 작업 디렉터리는 `~/Repository/playground/codex`이다. 기존
  `cmd-dev-tools`와 별도인 `paseo-sbx` 디렉터리에 새 프로젝트를 둔다.
- 이 작업 디렉터리와 `cmd-dev-tools`에는 Git 저장소와 `.codegraph/`가 없다.
- 호스트는 Apple silicon, macOS 26.6.2이다.
- Paseo 0.10.1, Docker 29.4.0, Codex CLI가 설치되어 있다.
- Docker의 현재 context는 OrbStack이며 데몬에 읽기 전용으로 접속하여
  서버 버전 29.4.0을 확인했다. 이는 sbx runtime 설치 확인과는 별개다.
- `sbx` 실행 파일과 Homebrew keg가 없다. Docker CLI에도 `sandbox` 명령이 없다.
- 설치된 Paseo의 `app.asar`에서 provider 설정 스키마와 Claude 실행 경로를 읽었다.
  `extends`, `label`, `command: string[]`, `paseoTools.enabled`가 지원된다.
- 현재 Docker 공식 문서에서 sbx는 독립 설치 도구이며, macOS 설치 명령은
  `brew install docker/tap/sbx`이다. sbx 사용에는 Docker 로그인이 필요하다.

## 접근 방식 비교와 선택

1. **기존 Paseo + custom provider 실행 wrapper:** 사용자가 선택한 방식이다.
   기존 UI와 agent 관리 기능을 유지하면서 에이전트 프로세스를 sandbox 안에서 실행한다.
   stdio, 작업 경로, 세션 저장 위치, 도구 주입을 맞춰야 한다.
2. **sandbox마다 Paseo daemon:** 도구 실행 경계가 단순하지만 연결 주소가 여섯 개가
   된다. 사용자가 기존 Paseo 한 곳을 선택했으므로 채택하지 않는다.
3. **Docker Compose 컨테이너:** 기존 설치 도구와 재사용하기 쉽지만 요청된 sbx
   runtime을 사용하지 않으므로 채택하지 않는다.

## 구성과 실행 흐름

공식 `docker.io/docker/sandbox-templates:codex`와
`docker.io/docker/sandbox-templates:claude-code`를 기반으로 여섯 이미지를 만든다.
공통 Dockerfile에 에이전트별 base image와 도구 선택을 전달한다. Bake 설정에는
여섯 target을 선언하여 개별 빌드와 전체 빌드를 지원한다.

Paseo custom provider의 `command`에는 이 프로젝트의 절대 경로 wrapper를 등록한다.
wrapper는 선택된 sandbox에 `sbx exec -i --workdir <현재 작업 경로>`로 접속하여
Codex app-server 또는 Claude Code 프로세스를 실행한다. TTY를 할당하지 않는다.
프로토콜 stdout에는 wrapper의 안내·로그를 출력하지 않는다. 오류는 stderr에 쓴다.

Paseo가 넘기는 인수와 stdin/stdout을 보존한다. Claude SDK가 호스트 실행 파일이나
JS entrypoint를 인수로 전달하는 경우는 설치된 Paseo의 실행 방식에 맞춰 처리한다.
임의 인수 문자열을 shell로 다시 평가하지 않는다. 프로세스 종료·중단 시 sandbox 안의
해당 agent 프로세스가 남지 않도록 종료 전파를 확인한다.

프로젝트를 sbx에 연결할 때 호스트와 sandbox의 절대 경로를 동일하게 유지한다.
선택한 프로젝트만 연결하며 서로 동시에 수정하는 작업은 프로젝트별 checkout 또는
worktree를 사용한다. 새 프로젝트는 기존 sandbox의 mount를 임의 변경하지 않고
프로젝트별 sandbox 이름과 provider 묶음을 생성한다.

## Paseo 도구와 세션 경계

사용자가 자동 위임을 포함하도록 요청했다. 호스트 Paseo의 MCP 주입을 활성화하고,
wrapper에서 Paseo MCP endpoint를 세션별 제한된 HTTP 중계로 교체한다. 호스트의
Paseo bearer token은 중계 프로세스 메모리에만 유지하고 sandbox에는 중계용 임시
capability를 전달한다. 원래 Paseo MCP 주소와 credential을 그대로 노출하지 않는다.

중계가 제공하는 도구는 provider/model 조회, 하위 agent 생성, 자신과 생성한 하위
agent의 상태·활동 조회, prompt 전송, 실행 중단으로 제한한다. 생성 대상 provider는
여섯 sandbox provider로 제한한다. 다른 workspace나 cwd 지정, 임의 설정·MCP 주입,
호스트 터미널·스크립트·브라우저·스케줄 실행은 거부한다. 조회 결과에서도 호스트
provider와 관련 없는 agent를 제거한다. 중계의 tools/list와 tools/call 양쪽에 같은
정책을 적용한다. Paseo provider 정책에서도 호스트 작업 도구를 비활성화한다.

sandbox에서 호스트 중계는 `host.docker.internal`로 접속한다. 세션이 시작할 때
해당 sandbox에만 중계 포트의 localhost 접근을 허용하고, 세션이 끝나면 해당 규칙을
제거한다. 중계는 호스트 loopback에만 bind한다. 사용자는 기존 Paseo UI/CLI에서
위임 관계와 결과를 확인한다. 자식 에이전트에도 동일한 wrapper와 중계 정책을 적용한다.

Codex·Claude의 로그인 정보, 세션과 사용자 설정은 각 sandbox 안에서 유지한다.
호스트의 사용자 홈 전체를 mount하지 않는다. Paseo의 세션 목록·재연결·로그 표시가
원격 agent 상태와 호환되는지 확인하고, 호스트 파일을 직접 읽는 기능은 지원 범위를
문서화한다. 읽기 실패를 정상 지원으로 보고하지 않는다.

## 도구 설치와 버전 관리

- Superpowers: `obra/superpowers`의 고정 revision을 사용한다. 에이전트가 지원하는
  native plugin 방식으로 skills와 hooks를 설치한다.
- Ponytail: `DietrichGebert/ponytail`의 고정 revision을 사용하고 native plugin을
  설치한다. Node.js 기반 lifecycle hooks를 함께 포함한다.
- GSD Core: `@opengsd/gsd-core`의 고정 npm 버전을 사용하고 공식 installer의
  `--codex --global` 또는 `--claude --global`을 실행한다. 파일을 단순 복사하여
  에이전트별 변환을 생략하지 않는다.
- base image digest와 도구 version/revision을 lock 파일에 기록한다. 아직 조회하지
  않은 버전값은 설치 단계에서 조회하여 고정하고 결과를 기록한다.
- 필요한 Codex·Claude CLI 버전을 base image에서 확인하고 설치 도구의 요구 버전에
  미달하면 호환되는 버전을 명시적으로 설치한다.
- hooks의 사용자 trust가 필요한 경우 등록과 실제 활성화를 구분하여 기록한다.
  자동 등록만으로 hooks가 동작한다고 보고하지 않는다.

## 템플릿과 GitHub 재사용

GitHub 저장소에는 Dockerfile, lock 파일, Bake 설정, 운영 CLI, Paseo 설정 생성 도구,
설치·복구 문서를 보관한다. 빌드 context는 이 프로젝트의 소스만 포함한다.
개인 인증 파일, Paseo 설정 원본, 세션, sandbox snapshot tar는 Git에 포함하지 않는다.

호스트 Docker image store와 sbx image store는 별개다. 로컬 빌드 결과는
`docker save`로 내보낸 뒤 `sbx template load`로 가져온다. 가져온 로컬 이미지는
`--pull never`와 동일 태그를 사용한다. 인증 전의 깨끗한 이미지로 여섯 sandbox를
생성하여 runtime 확인을 수행한다.

추후 GHCR을 사용하면 여섯 이미지를 `ghcr.io/<owner>/<repository>/<profile>:<version>`
형태로 게시한다. GitHub Actions는 여섯 target을 빌드할 수 있도록 작성하되, 게시 작업은
명시적인 release 실행에서 수행한다. 저장소 이름·소유자·공개 여부는 게시 시 지정한다.
Apple silicon과 x86 환경의 재사용을 위해 두 아키텍처의 base image 지원을 확인하고,
지원되는 플랫폼만 workflow에 선언한다.

실행 후 snapshot을 저장하는 기능과 최초 배포 템플릿을 구분한다. 최초 배포 템플릿은
빌드 소스로 생성하며 사용자 인증 상태가 들어간 sandbox snapshot을 배포하지 않는다.
sbx가 제공하는 secret proxy/OAuth를 이용하여 실행 시 인증한다.

## 로컬 산출물과 운영 명령의 책임

- `Dockerfile`, `docker-bake.hcl`: 공통 설치 로직과 여섯 이미지 target.
- `versions.lock.json`: base image와 도구 version/revision.
- `scripts/`: 도구 설치, 이미지 빌드·sbx import, 환경 생성, protocol wrapper.
- `paseo-sbx`: prerequisites 확인, build, create, list, shell, stop, provider 설정
  생성·적용 명령. 제거는 사용자가 선택한 sandbox에만 수행한다.
- `.github/workflows/`: 재빌드와 선택적인 GHCR 게시.
- `README.md`: 새 머신 설치, 프로젝트 연결, 인증, Paseo 사용, 복구, 알려진 제약.

Paseo 설정 적용 전 기존 설정을 백업하고 새 여섯 provider 항목만 병합한다.
경로와 설정값을 검증하고 실패하면 기존 설정을 유지한다. 생성된 설정을 먼저 로컬에서
검토할 수 있게 한다. provider 변경은 `paseo reload`로 반영한다.

## 오류 처리와 확인 기준

sbx 미설치, Docker 접근 실패, 로그인 필요, 이미지 미등록, 작업 경로 불일치,
설치 실패는 원인을 표시하고 해당 단계에서 멈춘다. 재실행 시 이미 만들어진 환경과
사용자 세션을 삭제하지 않는다. 네트워크 정책은 필요한 GitHub/npm/provider endpoint를
명시하고 기존 전역 정책을 무조건 allow-all로 바꾸지 않는다.

성공 기준은 다음과 같다.

1. 여섯 이미지가 고정된 도구 버전으로 빌드되고 sbx template 목록에 등록된다.
2. 여섯 sandbox 안에서 선택된 도구의 skills/plugin 설치를 확인한다.
3. 기존 Paseo에서 여섯 provider를 선택할 수 있다.
4. Codex app-server와 Claude stream 프로토콜이 실제 sandbox 안에서 연결된다.
5. 프로젝트 파일 작업, 중단, 재연결이 선택한 sandbox에서 동작한다.
6. 인증이 필요한 실제 모델 실행은 사용자 로그인 후 별도로 확인하며, 단순 설치
   확인과 구별한다. 로그인 미완료 상태를 실제 모델 실행 성공으로 보고하지 않는다.
7. 새 머신에서 소스로 재빌드하거나 registry 이미지로 다시 만드는 방법이 문서화된다.

## 참조

- [Docker SBX 설치](https://docs.docker.com/ai/sandboxes/install/)
- [SBX 사용과 template import/export](https://docs.docker.com/ai/sandboxes/usage/)
- [SBX exec](https://docs.docker.com/reference/cli/sbx/exec/)
- [Paseo custom providers](https://paseo.sh/docs/custom-providers)
- [Superpowers](https://github.com/obra/superpowers)
- [GSD Core 에이전트별 설치](https://github.com/open-gsd/gsd-core/blob/main/docs/how-to/install-on-your-runtime.md)
- [Ponytail](https://github.com/DietrichGebert/ponytail)
- [OpenAI plugin 설치와 구성](https://developers.openai.com/plugins/build/plugins)
