import { useCallback, useEffect, useMemo, useState } from "react";
import * as Separator from "@radix-ui/react-separator";
import { Activity, Boxes, Braces, CheckCircle2, ChevronRight, CircleStop, Copy, Download, Folder, Globe2, MoreHorizontal, Plus, Radio, RadioTower, Search, Settings2, ShieldCheck, SlidersHorizontal, Trash2, Wifi, X, Zap } from "lucide-react";
import { MethodSelect } from "./components/MethodSelect";
import { Hint } from "./components/Hint";

type Project={id:string;name:string;domain:string;createdAt:string};
type Endpoint={id:string;projectId:string;method:string;scheme:string;host:string;path:string;status:number;requestHeaders:Record<string,string>;requestBody:string;responseHeaders:Record<string,string>;responseBody:string;contentType:string;durationMs:number;source:string;mocked:boolean;hitCount:number;lastSeenAt:string};
type MockRule={id:string;endpointId:string;projectId:string;method:string;host:string;path:string;status:number;headers:Record<string,string>;body:string;enabled:boolean};
type Recording={active:boolean;projectId:string;domain:string};
type ProxyStatus={supported:boolean;os:string;service:string;enabled:boolean;url:string;expectedUrl:string;managed:boolean;message?:string};
type CertificateStatus={exists:boolean;trusted:boolean;path:string;message:string};

const desktop=()=>"__TAURI_INTERNALS__" in window;
async function api<T>(path:string,init?:RequestInit):Promise<T>{
  if(desktop()){
    const {invoke}=await import("@tauri-apps/api/core");
    const body=typeof init?.body==="string"&&init.body?JSON.parse(init.body):null;
    return invoke<T>("api_call",{method:init?.method||"GET",path,body});
  }
  const r=await fetch(path,{...init,headers:{"Content-Type":"application/json",...(init?.headers||{})}});if(!r.ok)throw new Error(await r.text());return r.json()
}
async function revealCertificate(){if(desktop()){const {invoke}=await import("@tauri-apps/api/core");await invoke("reveal_certificate")}else{location.href="/api/ca"}}
async function getCertificateStatus(){if(!desktop())return {exists:false,trusted:false,path:"",message:"请在桌面应用中检查证书"};const {invoke}=await import("@tauri-apps/api/core");return invoke<CertificateStatus>("certificate_status")}
async function trustCertificate(){const {invoke}=await import("@tauri-apps/api/core");return invoke<CertificateStatus>("install_certificate")}
const methodTone=(m:string)=>({GET:"green",POST:"blue",PUT:"amber",PATCH:"purple",DELETE:"red"}[m]||"muted");
const pretty=(value:string)=>{try{return JSON.stringify(JSON.parse(value),null,2)}catch{return value}};

export function App(){
  const [projects,setProjects]=useState<Project[]>([]);const [selected,setSelected]=useState("");const [endpoints,setEndpoints]=useState<Endpoint[]>([]);const [mocks,setMocks]=useState<MockRule[]>([]);const [recording,setRecording]=useState<Recording>({active:false,projectId:"",domain:""});
  const [domain,setDomain]=useState("");const [query,setQuery]=useState("");const [activeEndpoint,setActiveEndpoint]=useState<Endpoint|null>(null);const [showProject,setShowProject]=useState(false);const [showManual,setShowManual]=useState(false);const [showProxySetup,setShowProxySetup]=useState(false);const [toast,setToast]=useState("");
  const project=projects.find(p=>p.id===selected);
  const load=useCallback(async()=>{const [ps,state,ms]=await Promise.all([api<Project[]>("/api/projects"),api<{recording:Recording}>("/api/state"),api<MockRule[]>("/api/mocks")]);setProjects(ps);setRecording(state.recording);setMocks(ms);setSelected(v=>v||ps[0]?.id||"")},[]);
  const loadEndpoints=useCallback(async(id:string)=>{if(!id){setEndpoints([]);return}setEndpoints(await api<Endpoint[]>(`/api/projects/${id}/endpoints`))},[]);
  useEffect(()=>{
    load().catch(showError);
    let refreshTimer:ReturnType<typeof setTimeout>|undefined;
    const scheduleRefresh=()=>{if(refreshTimer)clearTimeout(refreshTimer);refreshTimer=setTimeout(()=>load().catch(()=>{}),160)};
    if(desktop()){
      let unlisten:(()=>void)|undefined;
      import("@tauri-apps/api/event").then(({listen})=>listen("data-changed",scheduleRefresh)).then(fn=>unlisten=fn);
      return()=>{unlisten?.();if(refreshTimer)clearTimeout(refreshTimer)};
    }
    const es=new EventSource("/events");es.addEventListener("change",scheduleRefresh);return()=>{es.close();if(refreshTimer)clearTimeout(refreshTimer)}
  },[load]);
  useEffect(()=>{loadEndpoints(selected).catch(showError)},[selected,loadEndpoints,recording,mocks.length]);
  useEffect(()=>{if(project)setDomain(project.domain)},[project?.id,project?.domain]);
  useEffect(()=>{if(activeEndpoint){const fresh=endpoints.find(e=>e.id===activeEndpoint.id);if(fresh)setActiveEndpoint(fresh)}},[endpoints]);
  function showError(e:unknown){const message=e instanceof Error?e.message:typeof e==="string"?e:"操作失败";setToast(message||"操作失败");setTimeout(()=>setToast(""),5200)}
  async function toggleRecording(){if(!project)return;try{if(recording.active){await api("/api/recording",{method:"POST",body:JSON.stringify({active:false,projectId:"",domain:""})})}else{await api(`/api/projects/${project.id}`,{method:"PUT",body:JSON.stringify({name:project.name,domain})});await api("/api/recording",{method:"POST",body:JSON.stringify({active:true,projectId:project.id,domain})})}await load()}catch(e){showError(e)}}
  async function removeEndpoint(e:Endpoint){if(!confirm(`删除接口 ${e.path}？`))return;await api(`/api/endpoints/${e.id}`,{method:"DELETE"});setActiveEndpoint(null);await loadEndpoints(selected)}
  async function removeProject(p:Project){if(!confirm(`删除项目“${p.name}”及其中的所有接口和 Mock？`))return;try{await api(`/api/projects/${p.id}`,{method:"DELETE"});if(selected===p.id)setSelected("");await load()}catch(e){showError(e)}}
  async function makeMock(e:Endpoint){try{await api(`/api/endpoints/${e.id}/mock`,{method:"POST"});await load();await loadEndpoints(selected);setToast("已创建 Mock，下一次请求立即生效");setTimeout(()=>setToast(""),2500)}catch(err){showError(err)}}
  const filtered=useMemo(()=>endpoints.filter(e=>`${e.method} ${e.path}`.toLowerCase().includes(query.toLowerCase())),[endpoints,query]);
  const currentMock=activeEndpoint?mocks.find(m=>m.endpointId===activeEndpoint.id):undefined;
  const mockedCount=endpoints.filter(e=>e.mocked).length;
  const mockCoverage=endpoints.length?Math.round(mockedCount/endpoints.length*100):0;
  return <div className="shell">
    <div className="windowChrome" data-tauri-drag-region>
      <div className="chromeSidebar" data-tauri-drag-region><span className="chromeLogo"><Zap size={13}/></span><strong data-tauri-drag-region>Max Proxy</strong></div>
      <div className="chromeMain" data-tauri-drag-region><div className="chromePath" data-tauri-drag-region><span data-tauri-drag-region>Workspace</span><ChevronRight size={12}/><b data-tauri-drag-region>{project?.name||"未选择项目"}</b></div><div className={`connection ${recording.active?"live":""}`}><i/>{recording.active?`正在录制 ${recording.domain}`:"代理已就绪"}</div></div>
    </div>
    <aside className="sidebar">
      <div className="workspaceMeta"><span>Local workspace</span><strong>API Contract Lab</strong><small>DESKTOP · RUST ENGINE</small></div>
      <div className="sideTitle"><span>项目</span><button className="iconBtn" onClick={()=>setShowProject(true)} aria-label="新建项目"><Plus size={17}/></button></div>
      <nav>{projects.map(p=><button key={p.id} className={`projectItem ${selected===p.id?"active":""}`} onClick={()=>setSelected(p.id)}><span className="projectIcon"><Folder size={16}/></span><span><b>{p.name}</b><small>{p.domain||"尚未设置域名"}</small></span><span className="projectMore" role="button" title="删除项目" onClick={e=>{e.stopPropagation();removeProject(p)}}><Trash2 size={13}/></span></button>)}</nav>
      {!projects.length&&<div className="emptySide">创建第一个项目<br/>开始录制接口</div>}
      <div className="sideFooter"><button onClick={()=>setShowProxySetup(true)}><Settings2 size={15}/>代理设置助手</button><button onClick={()=>revealCertificate().catch(showError)}><Download size={15}/> 定位 HTTPS 证书</button><span>Proxy · 127.0.0.1:8899</span></div>
    </aside>
    <main>
      <header className="workspaceHero">
        <div className="heroIdentity"><span className="heroMark"><Braces size={22}/></span><div className="heroCopy"><p className="eyebrow">PROJECT WORKSPACE</p><h1>{project?.name||"选择一个项目"}</h1><p>{project?.domain||"创建项目后即可录制、Mock 并校验接口契约"}</p></div></div>
        <div className="heroActions">{project?.domain&&<span className="targetPill"><i/>{project.domain}</span>}<Separator.Root className="heroSeparator" orientation="vertical"/><Hint content="打开代理、PAC 与 HTTPS 证书设置"><button className="heroSetupButton" onClick={()=>setShowProxySetup(true)} aria-label="连接设置"><SlidersHorizontal size={18}/></button></Hint></div>
      </header>
      <section className="overviewGrid" aria-label="项目概览">
        <article className="metricCard"><span className="metricIcon violet"><Boxes size={18}/></span><div><span className="metricLabel">已捕获接口</span><strong>{endpoints.length}</strong><small>按 Method + Path 去重</small></div></article>
        <article className="metricCard"><span className="metricIcon lime"><Zap size={18}/></span><div><span className="metricLabel">生效 Mock</span><strong>{mockedCount}</strong><small>{mockCoverage}% 接口已接管</small></div><span className="metricTrack"><i style={{width:`${mockCoverage}%`}}/></span></article>
        <article className={`metricCard captureState ${recording.active?"active":""}`}><span className="metricIcon coral"><RadioTower size={18}/></span><div><span className="metricLabel">流量录制</span><strong>{recording.active?"LIVE":"IDLE"}</strong><small>{recording.active?recording.domain:"等待开始录制"}</small></div><span className="livePulse"/></article>
      </section>
      <section className={`recorder recorderConsole ${recording.active?"isRecording":""}`}>
        <div className="recIcon"><Globe2 size={21}/></div><div className="domainField"><label><span>CAPTURE TARGET</span>{recording.active&&<em>LIVE</em>}</label><input value={domain} onChange={e=>setDomain(e.target.value)} placeholder="api.example.com" disabled={!project||recording.active}/><small>仅捕获此域名及子域名，请求按接口 Path 持续补录并自动去重</small></div>
        <button className={`recordBtn ${recording.active?"stop":""}`} disabled={!project||(!recording.active&&!domain)} onClick={toggleRecording}>{recording.active?<><CircleStop size={18}/>停止录制</>:<><Radio size={18}/>开始录制</>}</button>
      </section>
      <section className="contentCard">
        <div className="toolbar"><div className="directoryTitle"><span className="sectionKicker">ENDPOINT DIRECTORY</span><h2>接口目录 <b>{endpoints.length}</b></h2><p>{mockedCount} 个 Mock 正在管理前后端响应协议</p></div><div className="tools"><label className="search"><Search size={16}/><input value={query} onChange={e=>setQuery(e.target.value)} placeholder="搜索 Method 或 Path"/></label><Hint content="不经过录制，直接创建一个接口"><button className="secondary" disabled={!project} onClick={()=>setShowManual(true)}><Plus size={16}/>手动添加</button></Hint></div></div>
        <div className="tableHead"><span>接口</span><span>来源</span><span>最近状态</span><span>耗时</span><span>命中</span><span/></div>
        <div className="endpointList">{filtered.map(e=><button className="endpointRow" key={e.id} onClick={()=>setActiveEndpoint(e)}><span className="endpointName"><b className={`method ${methodTone(e.method)}`}>{e.method}</b><code>{e.path}</code>{e.mocked&&<em>MOCK</em>}</span><span className="source">{e.source==="manual"?"手动":"录制"}</span><span className={`status ${e.status<400?"ok":"bad"}`}><i/>{e.status}</span><span>{e.durationMs} ms</span><span>{e.hitCount} 次</span><MoreHorizontal size={17}/></button>)}</div>
        {!filtered.length&&<div className="empty"><div className="emptyOrb"><Activity size={27}/><i/></div><span className="emptyKicker">NO ENDPOINTS YET</span><h3>{project?"等待第一条接口流量":"从一个项目开始"}</h3><p>{project?"设置目标域名并开启录制，再刷新你的业务页面。捕获结果会自动出现在这里。":"项目会隔离不同应用的接口记录、Mock 响应与契约状态。"}</p><div className="emptyActions">{project?<><button className="primary" disabled={recording.active||!domain} onClick={toggleRecording}><Radio size={16}/>开始录制</button><button className="secondary" onClick={()=>setShowManual(true)}><Plus size={16}/>手动添加</button></>:<button className="primary" onClick={()=>setShowProject(true)}><Plus size={16}/>新建项目</button>}</div></div>}
      </section>
    </main>
    {activeEndpoint&&<EndpointPanel endpoint={activeEndpoint} mock={currentMock} onClose={()=>setActiveEndpoint(null)} onMock={()=>makeMock(activeEndpoint)} onDelete={()=>removeEndpoint(activeEndpoint)} onSaved={async()=>{await load();await loadEndpoints(selected)}} onError={showError}/>}
    {showProject&&<ProjectModal onClose={()=>setShowProject(false)} onCreate={async(name,d)=>{const p=await api<Project>("/api/projects",{method:"POST",body:JSON.stringify({name,domain:d})});setShowProject(false);await load();setSelected(p.id)}}/>}
    {showManual&&project&&<ManualModal domain={project.domain} onClose={()=>setShowManual(false)} onCreate={async data=>{await api(`/api/projects/${project.id}/endpoints`,{method:"POST",body:JSON.stringify(data)});setShowManual(false);await loadEndpoints(project.id)}}/>}
    {showProxySetup&&<ProxySetupModal onClose={()=>setShowProxySetup(false)} onError={showError}/>}
    {toast&&<div className="toast">{toast}</div>}
  </div>
}

function EndpointPanel({endpoint,mock,onClose,onMock,onDelete,onSaved,onError}:{endpoint:Endpoint;mock?:MockRule;onClose:()=>void;onMock:()=>void;onDelete:()=>void;onSaved:()=>void;onError:(e:unknown)=>void}){
  const [status,setStatus]=useState(mock?.status||endpoint.status);const [body,setBody]=useState(pretty(mock?.body??endpoint.responseBody));
  useEffect(()=>{setStatus(mock?.status||endpoint.status);setBody(pretty(mock?.body??endpoint.responseBody))},[endpoint.id,mock?.id]);
  async function save(){if(!mock)return;try{await api(`/api/mocks/${mock.id}`,{method:"PATCH",body:JSON.stringify({status,body})});await onSaved()}catch(e){onError(e)}}
  return <div className="overlay" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><aside className="panel"><div className="panelTop"><div><span className={`method ${methodTone(endpoint.method)}`}>{endpoint.method}</span><h2>{endpoint.path}</h2><p>{endpoint.scheme}://{endpoint.host}{endpoint.path}</p></div><button className="iconBtn" onClick={onClose}><X size={20}/></button></div>
    <div className="metaGrid"><div><span>状态</span><b>{endpoint.status}</b></div><div><span>耗时</span><b>{endpoint.durationMs} ms</b></div><div><span>捕获次数</span><b>{endpoint.hitCount}</b></div></div>
    <div className="panelSection"><div className="sectionTitle"><h3>{mock?"Mock 响应":"最后一次真实响应"}</h3>{mock&&<label className="switch"><input type="checkbox" checked={mock.enabled} onChange={async e=>{await api(`/api/mocks/${mock.id}`,{method:"PATCH",body:JSON.stringify({enabled:e.target.checked})});onSaved()}}/><span/></label>}</div>
      {mock&&<label className="statusInput">HTTP 状态码<input type="number" value={status} onChange={e=>setStatus(Number(e.target.value))}/></label>}
      <textarea className="codeEditor" spellCheck={false} value={body} readOnly={!mock} onChange={e=>setBody(e.target.value)}/>
    </div>
    <div className="panelActions">{mock?<button className="primary" onClick={save}>保存并立即生效</button>:<button className="primary" onClick={onMock}><Zap size={16}/>使用此响应创建 Mock</button>}<button className="dangerText" onClick={onDelete}><Trash2 size={16}/>删除接口</button></div>
  </aside></div>
}

function ProjectModal({onClose,onCreate}:{onClose:()=>void;onCreate:(name:string,domain:string)=>void}){const [name,setName]=useState("");const [domain,setDomain]=useState("");return <Modal title="新建项目" onClose={onClose}><label>项目名称<input autoFocus value={name} onChange={e=>setName(e.target.value)} placeholder="例如：商城 Web"/></label><label>默认域名<input value={domain} onChange={e=>setDomain(e.target.value)} placeholder="api.example.com"/><small>稍后也可以在录制栏修改</small></label><button className="primary full" disabled={!name.trim()} onClick={()=>onCreate(name,domain)}>创建项目</button></Modal>}
function ManualModal({domain,onClose,onCreate}:{domain:string;onClose:()=>void;onCreate:(d:object)=>void}){const [method,setMethod]=useState("GET");const [path,setPath]=useState("");const [status,setStatus]=useState(200);const [responseBody,setBody]=useState("{\n  \"code\": 0,\n  \"data\": {}\n}");return <Modal title="手动添加接口" onClose={onClose}><div className="inlineFields"><label>方法<MethodSelect value={method} onValueChange={setMethod}/></label><label>状态码<input className="statusCodeInput" type="number" min={100} max={599} value={status} onChange={e=>setStatus(Number(e.target.value))}/></label></div><label>接口 Path<input value={path} onChange={e=>setPath(e.target.value)} placeholder="/api/users/1001"/><small>{domain||"当前项目尚未设置域名"}</small></label><label>响应 Body<textarea className="modalCode" value={responseBody} onChange={e=>setBody(e.target.value)}/></label><button className="primary full" disabled={!path.trim()} onClick={()=>onCreate({method,path,status,responseBody})}>添加到接口目录</button></Modal>}
function ProxySetupModal({onClose,onError}:{onClose:()=>void;onError:(e:unknown)=>void}){
  const [status,setStatus]=useState<ProxyStatus|null>(null);const [certificate,setCertificate]=useState<CertificateStatus|null>(null);const [busy,setBusy]=useState(false);const [certificateBusy,setCertificateBusy]=useState(false);const [copied,setCopied]=useState(false);const [feedback,setFeedback]=useState("");const [actionError,setActionError]=useState("");
  const refresh=useCallback(()=>Promise.all([api<ProxyStatus>("/api/system-proxy"),getCertificateStatus()]).then(([proxy,cert])=>{setStatus(proxy);setCertificate(cert)}).catch(onError),[onError]);useEffect(()=>{refresh()},[refresh]);
  async function act(action:"enable"|"restore"){setBusy(true);setActionError("");setFeedback(action==="enable"?"正在写入 macOS 自动代理配置…":"正在恢复之前的代理配置…");try{const next=await api<ProxyStatus>("/api/system-proxy",{method:"POST",body:JSON.stringify({action})});setStatus(next);setFeedback(action==="enable"?"PAC 已启用，浏览器流量现在可以进入本地代理。":"系统代理已恢复。")}catch(e){const message=e instanceof Error?e.message:typeof e==="string"?e:"系统代理配置失败";setActionError(message||"系统代理配置失败");setFeedback("");onError(e)}finally{setBusy(false)}}
  async function installCert(){setCertificateBusy(true);setActionError("");setFeedback("正在将 Max Proxy Mock 根证书加入用户钥匙串…");try{const next=await trustCertificate();setCertificate(next);setFeedback("证书已受信任。请完全退出并重新打开 Chrome，然后刷新业务页面。")}catch(e){const message=e instanceof Error?e.message:typeof e==="string"?e:"证书安装失败";setActionError(message||"证书安装失败");setFeedback("");onError(e)}finally{setCertificateBusy(false)}}
  async function copy(){if(!status)return;await navigator.clipboard.writeText(status.expectedUrl);setCopied(true);setTimeout(()=>setCopied(false),1600)}
  return <div className="overlay modalOverlay" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><div className="modal proxyModal"><div className="modalTitle"><div><p className="eyebrow">首次使用向导</p><h2>连接浏览器流量</h2></div><button className="iconBtn" onClick={onClose}><X size={19}/></button></div><p className="setupIntro">录制按钮负责筛选接口，PAC 负责让目标域名的流量真正经过本地代理。</p><div className={`proxyState ${status?.managed?"ready":""}`}><div className="stateIcon">{status?.managed?<CheckCircle2 size={21}/>:<Wifi size={21}/>}</div><div><b>{status?.managed?"系统 PAC 已连接":"系统 PAC 尚未连接"}</b><span>{status?.supported?`当前网络：${status.service}`:(status?.message||"正在检测系统设置…")}</span></div>{status?.supported&&(status.managed?<button className="ghostDanger" disabled={busy} onClick={()=>act("restore")}>{busy?"正在恢复…":"恢复原设置"}</button>:<button className="primary" disabled={busy} onClick={()=>act("enable")}>{busy?"正在配置…":"一键启用 PAC"}</button>)}</div>{(feedback||actionError)&&<div className={`actionFeedback ${actionError?"error":"success"}`} role="status" aria-live="polite">{actionError||feedback}</div>}<div className="setupSteps"><section><i>1</i><div><h3>配置系统代理</h3><p>只代理项目中配置的域名，其他网站保持直连。</p><div className="pacLine"><code>{status?.expectedUrl||"http://127.0.0.1:8900/proxy.pac"}</code><button onClick={copy}>{copied?<CheckCircle2 size={15}/>:<Copy size={15}/>} {copied?"已复制":"复制"}</button></div>{!status?.supported&&<p className="manualHint">请在系统网络设置的“自动代理配置”中粘贴此地址。</p>}</div></section><section className={certificate?.trusted?"certReady":"certMissing"}><i>{certificate?.trusted?<CheckCircle2 size={14}/>:2}</i><div><h3>{certificate?.trusted?"HTTPS 证书已受信任":"信任 HTTPS 证书"}</h3><p>{certificate?.message||"正在检查 macOS 钥匙串…"}</p><div className="certActions">{certificate&&!certificate.trusted&&<button className="primary" disabled={certificateBusy||!certificate.exists} onClick={installCert}><ShieldCheck size={15}/>{certificateBusy?"正在安装…":"安装并信任证书"}</button>}<button className="guideLink" onClick={()=>revealCertificate().catch(onError)}>在 Finder 中查看</button></div></div></section><section><i>3</i><div><h3>开始录制</h3><p>{certificate?.trusted?"证书已就绪。开始录制后刷新你的业务页面。":"先完成证书信任，再开始录制并刷新业务页面。"}</p></div></section></div><div className="setupNote">根证书和私钥只保存在本机应用数据目录中，仅用于你主动配置的 Mock 域名。</div></div></div>
}
function Modal({title,onClose,children}:{title:string;onClose:()=>void;children:React.ReactNode}){return <div className="overlay modalOverlay" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><div className="modal"><div className="modalTitle"><h2>{title}</h2><button className="iconBtn" onClick={onClose}><X size={19}/></button></div>{children}</div></div>}
