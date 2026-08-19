import React from "react";
import ReactDOM from "react-dom/client";
import * as Tooltip from "@radix-ui/react-tooltip";
import { App } from "./App";
import "./styles.css";

class AppBoundary extends React.Component<{children:React.ReactNode},{error:string}>{
  state={error:""};
  static getDerivedStateFromError(error:unknown){return {error:error instanceof Error?error.message:String(error)}}
  componentDidCatch(error:unknown){console.error("UI render failed",error)}
  render(){return this.state.error?<div className="fatalError"><strong>界面组件加载失败</strong><span>{this.state.error}</span><button onClick={()=>location.reload()}>重新加载</button></div>:this.props.children}
}

ReactDOM.createRoot(document.getElementById("root")!).render(<React.StrictMode><AppBoundary><Tooltip.Provider delayDuration={380} skipDelayDuration={100}><App /></Tooltip.Provider></AppBoundary></React.StrictMode>);
