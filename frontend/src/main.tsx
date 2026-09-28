import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
// xterm 的样式必须全局引一次（终端容器 .xterm-host 的补充规则在 style.css 里）
import '@xterm/xterm/css/xterm.css'
import App from './App'

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
)
