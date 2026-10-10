import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
// xterm 的样式必须全局引一次（终端容器 .xterm-host 的补充规则在 style.css 里）
import '@xterm/xterm/css/xterm.css'
import i18next from 'i18next'
import App from './App'
import {ErrorBoundary} from './components/ui/error-boundary'
import {getLanguage, loadExternalLocales, onLanguageChanged} from './lib/api'
import {initI18n, initI18nBuiltin} from './i18n'
import {useAppStore} from './state/store'

const container = document.getElementById('root')

const root = createRoot(container!)

// 启动接线：render 前先取语言配置与外部语言包并 await initI18n，
// 避免首帧资源未就绪时文案闪烁/退回 key。
async function bootstrap() {
    try {
        const [lang, ext] = await Promise.all([getLanguage(), loadExternalLocales()])
        // 语言统一用 Go 侧 resolved（applang 解析），不走前端 navigator：
        // 两套解析（navigator vs OS 首选语言）可能分歧，导致界面与托盘语言不一致。
        // resolved 永不为 'system'，initI18n 的直通语义不变。
        await initI18n(lang.resolved, ext)
        useAppStore.getState().setLanguage(lang)
    } catch (e) {
        // 失败不阻塞启动：initI18nBuiltin 兜底，只保证内置资源可用、应用可启动
        //（不承诺 initI18n 中途失败的完美恢复）
        console.warn('[i18n] bootstrap language wiring failed, using builtin default', e)
        initI18nBuiltin()
    }
    // 事件订阅单独守卫：window.runtime 缺失时 EventsOn 抛 TypeError，
    // 不能让它打断 bootstrap 在 render 之前（白屏）。事件里切语言同样用 Go resolved。
    try {
        onLanguageChanged((info) => {
            useAppStore.getState().setLanguage(info)
            void i18next.changeLanguage(info.resolved)
        })
    } catch (e) {
        console.warn('[i18n] language:changed subscribe failed, startup not blocked', e)
    }

    root.render(
        <React.StrictMode>
            <ErrorBoundary>
                <App/>
            </ErrorBoundary>
        </React.StrictMode>
    )
}

void bootstrap()
