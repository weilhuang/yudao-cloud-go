import DefaultTheme from 'vitepress/theme'
import { h, onBeforeUnmount, onMounted } from 'vue'
import './style.css'

// Mermaid 默认把很宽的图压到正文宽度，文字会小到看不清。
// 图绘制完成后按 viewBox 给一个可读的最小宽度；窄屏可横向滚动。
const Layout = {
  setup() {
    let observer: MutationObserver | undefined
    onMounted(() => {
      const resizeDiagrams = () => {
        document.querySelectorAll<SVGSVGElement>('.mermaid svg').forEach((svg) => {
          const width = svg.viewBox.baseVal.width
          if (width > 720) svg.style.minWidth = `${Math.min(Math.ceil(width * 0.85), 1600)}px`
        })
      }
      resizeDiagrams()
      observer = new MutationObserver(resizeDiagrams)
      observer.observe(document.querySelector('#app') ?? document.body, { childList: true, subtree: true })
    })
    onBeforeUnmount(() => observer?.disconnect())
    return () => h(DefaultTheme.Layout)
  },
}

export default { extends: DefaultTheme, Layout }
