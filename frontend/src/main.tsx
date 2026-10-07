import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
// xterm 的样式必须全局引一次（终端容器 .xterm-host 的补充规则在 style.css 里）
import '@xterm/xterm/css/xterm.css'
import i18next from 'i18next'
import App from './App'
import {getLanguage, loadExternalLocales, onLanguageChanged} from './lib/api'
import {initI18n, initI18nBuiltin, resolveLanguage} from './i18n'
import {useAppStore} from './state/store'

const container = document.getElementById('root')

const root = createRoot(container!)

// 启动接线：render 前先取语言配置与外部语言包并 await initI18n，
// 避免首帧资源未就绪时文案闪烁/退回 key。接线失败不阻塞启动（initI18nBuiltin 兜底）。
async function bootstrap() {
    try {
        const [lang, ext] = await Promise.all([getLanguage(), loadExternalLocales()])
        await initI18n(lang.configured, ext)
        useAppStore.getState().setLanguage(lang)
    } catch (e) {
        console.warn('[i18n] 启动语言接线失败，使用内置默认语言', e)
        initI18nBuiltin()
    }
    // 语言变更事件（Go 侧 SetLanguage 广播）：同步 store + 切换 i18n 语言。
    // 设置页选中态读 store，不在组件里二次写入，避免双写。
    onLanguageChanged((info) => {
        useAppStore.getState().setLanguage(info)
        void i18next.changeLanguage(resolveLanguage(info.configured))
    })

    root.render(
        <React.StrictMode>
            <App/>
        </React.StrictMode>
    )
}

void bootstrap()
