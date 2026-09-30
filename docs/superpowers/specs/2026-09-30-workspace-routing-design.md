# 작업 위치 기반 sandbox 연결 설계

상태: 사용자가 채팅에서 설계를 승인했다(2026-09-30). 구현 및 검증 기록은 저장소의 구현 기록에 남긴다.

## 목표

여러 프로젝트가 같은 조합을 사용할 수 있게 한다. 예를 들어 A·B·C는 superpowers(codex, claude), D는 gsd-core(codex, claude), E는 ponytail(codex, claude)을 사용한다. Paseo에서는 조합 이름으로 provider를 선택하고, 실행 시 작업 위치의 프로젝트 sandbox에 연결한다.

현재는 provider 이름마다 마지막으로 등록한 workspace 하나만 연결된다. 같은 조합을 여러 프로젝트에서 동시에 사용할 수 없다.

## 유지하는 동작

- sandbox는 `create --workspace`로 명시적으로 생성한다. 실행 중 자동으로 생성하지 않는다.
- sandbox는 프로젝트와 조합마다 따로 두며, 해당 workspace만 mount한다.
- workspace별 registry(`~/.local/share/paseo-sbx/<hash>/registry.json`)와 sandbox 이름 규칙을 유지한다.
- 작업 위치가 어느 등록 workspace에도 속하지 않으면 거부한다.
- 하위 agent 위임은 호출한 agent의 workspace에 등록된 조합으로만 제한한다.

## 구성

### 공용 launcher

- `~/.local/share/paseo-sbx/launcher/agent-wrapper`에 공용 launcher 하나를 설치한다.
- Paseo provider 명령은 `[<공용 launcher>, <조합 이름>]`이다. provider 이름에 프로젝트를 붙이지 않는다.
- Paseo 설정의 provider 목록은 모든 registry의 활성 조합을 합친 것이다. 5개 프로젝트 예시에서는 여섯 개가 등록된다.

### 실행 시 연결 규칙

1. 공용 launcher는 `~/.local/share/paseo-sbx/*/registry.json`을 읽는다. 해당 조합이 활성인 registry만 후보로 삼는다.
2. 작업 위치를 정한다. `PASEO_AGENT_CWD` 환경 변수가 있으면 그 값을 쓰고, 없으면 프로세스 cwd를 쓴다. 이 경로를 canonical 경로로 변환하고, 이를 포함하는 workspace 후보를 찾는다.
3. 후보가 여럿이면 가장 깊은 workspace를 사용한다. 중첩 workspace를 등록한 경우에 해당한다.
4. 후보가 없으면 거부한다. 단, 다음 "workspace 밖 실행" 규칙은 예외로 처리한다.
5. 선택한 registry로 현재와 같은 검증, relay, `sbx exec`를 수행한다.

별도 색인 파일을 두지 않고 registry를 직접 읽는다. 따라서 `create`와 색인 사이의 불일치가 생기지 않는다.

### Paseo 실행 방식 (0.10.1 앱 번들에서 확인)

- Paseo는 agent 세션을 만들거나 재개할 때마다 provider 프로세스를 새로 실행한다. Codex의 경우 세션마다 `app-server`가 따로 뜬다. 세션 간에 프로세스를 공유하지 않으므로 A·B 프로젝트 세션은 동시에 각자의 sandbox로 실행된다.
- Codex `app-server`는 `cwd`를 지정하지 않고 실행된다. 따라서 프로세스 cwd는 항상 daemon cwd다. 프로젝트 경로는 RPC(`config/read`, `thread/start`의 `cwd`)로만 전달된다.
- 세션 실행 시 환경 변수에 `PASEO_AGENT_ID`와 `PASEO_AGENT_CWD`가 추가된다. 연결 규칙은 `PASEO_AGENT_CWD`를 우선 사용한다.
- 모델 조회, 가져오기 가능한 세션 목록, 보관 처리는 세션 환경 없이 `app-server`를 실행한다. 이 경우 `PASEO_AGENT_CWD`가 없다.

### workspace 밖 실행

`PASEO_AGENT_CWD`가 없고 프로세스 cwd도 등록 workspace 밖인 경우가 있다. Codex 모델 조회 등을 위한 `app-server`와 `--version` 조회가 이에 해당한다. 이 경우에는 해당 조합이 활성인 registry 중 workspace 경로 순으로 첫 번째를 사용한다. 기존과 같이 app-server의 시작 위치만 그 workspace로 바꾼다. RPC에 포함된 cwd는 선택된 workspace 안인지 계속 검사한다.

선택된 sandbox가 로그인되지 않았다면 모델 조회가 실패할 수 있다. 같은 조합의 sandbox는 모두 로그인해 두어야 한다.

## 보안 경계

- 공용 launcher와 모든 runtime을 포함하는 `~/.local/share/paseo-sbx`는 어떤 workspace 안에도 있으면 안 된다. `create`와 `register`에서 거부하고, 실행 시 선택된 workspace에 대해서도 다시 확인한다.
- 이 디렉터리와 registry는 호스트 CLI만 기록한다. 권한은 0700이다.
- 기존 검사도 유지한다. 대상은 소스 폴더 포함 금지, runtime이 workspace 밖에 있는지, RPC cwd 검사, relay 정책이다.
- relay가 허용하는 조합은 선택된 workspace registry의 활성 조합이다. A의 agent는 D의 gsd-core로 위임할 수 없다.

## 등록과 이전

- `register --apply`는 공용 launcher를 설치하거나 갱신한다. 이어서 모든 registry의 활성 조합으로 provider 목록을 다시 만든다.
- 조정 대상은 paseo-sbx가 관리하는 항목뿐이다. 명령이 공용 launcher 또는 `~/.local/share/paseo-sbx/<hash>/agent-wrapper`인 항목만 교체하거나 정리한다. 사용자가 직접 추가한 provider는 유지한다.
- 기존 workspace별 `agent-wrapper`는 이전 후 삭제한다. 기존 sandbox, 로그인 상태, 이미지는 그대로 재사용한다.
- 현재 설치된 runtime 네 개는 `make build` 후 `register --apply`를 한 번 실행하면 이전된다.
- `create --apply`도 같은 등록 과정을 사용한다.

## CLI 변경

- `list`에 `--all`을 추가한다. 모든 workspace와 각 workspace의 활성 조합을 보여준다.
- `unregister --workspace <path>`를 추가한다. 해당 registry의 활성 조합을 비워 연결 대상에서 제외하고 provider 목록을 다시 만든다. sandbox는 삭제하지 않는다.

## 사전 확인 결과

Paseo가 세션 간에 provider 프로세스를 공유하는지 앱 번들 코드에서 확인했다. 세션마다 새로 실행하므로 이 설계를 그대로 적용할 수 있다. 세부 내용은 "Paseo 실행 방식"에 정리했다. 실제 두 프로젝트 동시 세션 확인은 구현 후 검증 단계에서 수행한다.

## 검증

- 연결 규칙 단위 테스트: 단일 일치, 중첩 workspace의 가장 깊은 일치, 불일치 거부, symlink 경로, 비활성 조합 제외, workspace 밖 app-server와 `--version`, `PASEO_AGENT_CWD` 우선 적용.
- `PASEO_AGENT_CWD`와 RPC cwd가 서로 다른 workspace를 가리키면 거부하는지 확인한다.
- 등록 테스트: A·B·C·D·E 예시에서 provider 여섯 개가 모두 공용 launcher를 가리키는지 확인한다. 사용자 provider 유지, 기존 workspace별 항목 이전, `unregister` 후 목록도 확인한다.
- 보안 테스트: `~/.local/share/paseo-sbx`를 포함하는 workspace는 거부한다. 다른 workspace 조합으로의 relay 위임도 거부한다.
- 가짜 `sbx`와 `paseo`를 사용한 다중 프로젝트 시뮬레이션을 수행한다.
- 실제 환경 확인: 두 프로젝트에서 같은 조합을 선택해 세션을 시작하고, 각각 자기 sandbox에서 실행되는지 확인한다.
