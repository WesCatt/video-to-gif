import { useEffect, useMemo, useRef, useState } from "react";

const initialParams = {
  outputName: "clip",
  fps: "12",
  width: "",
  height: "",
  fitMode: "contain",
  background: "#0f172a",
  start: "0",
  duration: "0",
  speed: "1",
  loop: "0",
  maxColors: "128",
  dither: "sierra2_4a",
  paletteStatsMode: "full",
  diffMode: "rectangle",
  scaleAlgorithm: "lanczos",
  reverse: false
};

const PARAMS_STORAGE_KEY = "vtg-params";
const PARAMS_META_STORAGE_KEY = "vtg-params-meta";
const GIF_HISTORY_STORAGE_KEY = "vtg-gif-history";

const timingFieldDefs = [
  { id: "start", type: "number", step: "0.1", min: "0" },
  { id: "duration", type: "number", step: "0.1", min: "0" },
  { id: "speed", type: "number", step: "0.1", min: "0.1", max: "8" },
  { id: "fps", type: "number", step: "1", min: "1", max: "60" }
];

const outputFieldDefs = [
  { id: "outputName", type: "text", placeholder: "clip" },
  { id: "width", type: "number", step: "1", min: "0" },
  { id: "height", type: "number", step: "1", min: "0" },
  { id: "loop", type: "number", step: "1", min: "0" }
];

const dictionaries = {
  en: {
    brand: {
      title: "Video to GIF",
      caption: "Workbench"
    },
    languageLabel: "Language",
    status: {
      ready: "ffmpeg ready",
      missing: "ffmpeg missing"
    },
    actions: {
      light: "Light",
      dark: "Dark",
      render: "Render GIF",
      rendering: "Rendering...",
      reset: "Reset",
      download: "Download GIF",
      delete: "Delete",
      deleting: "Deleting..."
    },
    upload: {
      kicker: "Source file",
      titleIdle: "Drag a video here",
      titleLoaded: "Source ready",
      bodyIdle: "MP4, MOV, WEBM and other ffmpeg-readable formats are supported.",
      bodyLoaded: "Click to replace the current source clip.",
      overlay: "Drop the video anywhere to import it.",
      fallbackType: "unknown format",
      defaultChips: ["Drag", "Tune", "Render"]
    },
    preview: {
      sourceTitle: "Source",
      sourceWaiting: "Waiting",
      sourceReady: "Local file",
      sourceEmpty: "Upload a clip to preview it here before rendering.",
      resultTitle: "Rendered GIF",
      resultEmptyState: "No output yet",
      resultEmpty: "The generated GIF will appear here.",
      resultExpired: "This GIF has expired. Render again to get a fresh download link.",
      resultWindow: "24h download window",
      expiresAt: "Available until",
      resultAlt: "Rendered GIF preview"
    },
    form: {
      kicker: "Controls",
      title: "Conversion settings",
      sections: {
        timing: "Timing",
        output: "Size & loop",
        quality: "Palette & quality",
        result: "Result & log"
      },
      fields: {
        start: {
          label: "Start time",
          hint: "Seconds from the clip start."
        },
        duration: {
          label: "Duration",
          hint: "0 keeps the rest of the clip."
        },
        speed: {
          label: "Playback speed",
          hint: "1 is original speed."
        },
        fps: {
          label: "Frame rate",
          hint: "Higher FPS creates larger GIFs."
        },
        outputName: {
          label: "Output name",
          hint: "Used as the GIF filename prefix."
        },
        width: {
          label: "Width",
          hint: "Leave blank to preserve source width."
        },
        height: {
          label: "Height",
          hint: "Leave blank to preserve source height."
        },
        loop: {
          label: "Loop count",
          hint: "0 means infinite loop."
        },
        reverse: {
          label: "Reverse playback",
          hint: "Render frames in reverse order."
        },
        fitMode: {
          label: "Fit mode",
          hint: "Contain pads. Cover fills and crops."
        },
        background: {
          label: "Pad background",
          hint: "Used when contain adds empty canvas area."
        },
        maxColors: {
          label: "Max colors",
          hint: "GIF palettes are capped at 256 colors."
        },
        scaleAlgorithm: {
          label: "Scaling algorithm",
          hint: "Lanczos is sharper. Bilinear is lighter."
        },
        dither: {
          label: "Dither",
          hint: "Controls texture and gradients."
        },
        paletteStatsMode: {
          label: "Palette stats",
          hint: "How ffmpeg samples frames for the palette."
        },
        diffMode: {
          label: "Diff mode",
          hint: "Optimization for changing regions."
        }
      },
      options: {
        fitMode: {
          contain: "Contain",
          cover: "Cover",
          stretch: "Stretch",
          original: "Original ratio"
        },
        dither: {
          sierra2_4a: "Sierra 2-4A",
          floyd_steinberg: "Floyd-Steinberg",
          sierra2: "Sierra 2",
          bayer: "Bayer",
          heckbert: "Heckbert",
          none: "None"
        },
        paletteStatsMode: {
          full: "Full frame",
          diff: "Frame diff",
          single: "Single frame"
        },
        diffMode: {
          rectangle: "Rectangle diff",
          none: "No diff optimization"
        },
        scaleAlgorithm: {
          lanczos: "Lanczos",
          bicubic: "Bicubic",
          bilinear: "Bilinear",
          spline: "Spline",
          neighbor: "Nearest"
        }
      }
    },
    result: {
      sectionNote: "Generated GIFs stay available for 24 hours before automatic cleanup.",
      empty: "Run a job to inspect the ffmpeg commands.",
      job: "Job"
    },
    library: {
      title: "Active GIFs",
      empty: "No active GIFs for this browser yet.",
      expiresIn: "Expires in",
      expired: "Expired"
    },
    messages: {
      selectFile: "Select a video file before rendering.",
      ffmpegMissing: "Install ffmpeg or keep the local .tools version available before rendering.",
      deleteFailed: "Unable to delete this GIF right now."
    }
  },
  zh: {
    brand: {
      title: "视频转 GIF",
      caption: "工作台"
    },
    languageLabel: "语言",
    status: {
      ready: "ffmpeg 已就绪",
      missing: "ffmpeg 未就绪"
    },
    actions: {
      light: "浅色",
      dark: "深色",
      render: "生成 GIF",
      rendering: "生成中...",
      reset: "重置",
      download: "下载 GIF",
      delete: "删除",
      deleting: "删除中..."
    },
    upload: {
      kicker: "素材文件",
      titleIdle: "拖拽视频到这里",
      titleLoaded: "素材已就绪",
      bodyIdle: "支持 MP4、MOV、WEBM 以及其他 ffmpeg 可读取格式。",
      bodyLoaded: "点击可替换当前视频。",
      overlay: "把视频拖到页面任意位置即可导入。",
      fallbackType: "未知格式",
      defaultChips: ["拖入", "调整", "生成"]
    },
    preview: {
      sourceTitle: "源视频",
      sourceWaiting: "等待上传",
      sourceReady: "本地文件",
      sourceEmpty: "上传后可先在这里预览视频。",
      resultTitle: "生成结果",
      resultEmptyState: "暂无结果",
      resultEmpty: "生成后的 GIF 会显示在这里。",
      resultExpired: "这个 GIF 已过期，请重新生成新的下载链接。",
      resultWindow: "24 小时下载有效期",
      expiresAt: "有效至",
      resultAlt: "生成的 GIF 预览"
    },
    form: {
      kicker: "参数",
      title: "转换设置",
      sections: {
        timing: "时间",
        output: "尺寸与循环",
        quality: "调色与质量",
        result: "结果与命令"
      },
      fields: {
        start: {
          label: "开始时间",
          hint: "从视频开头起算，单位秒。"
        },
        duration: {
          label: "截取时长",
          hint: "填 0 表示保留剩余全部内容。"
        },
        speed: {
          label: "播放速度",
          hint: "1 为原速。"
        },
        fps: {
          label: "帧率",
          hint: "帧率越高，GIF 体积通常越大。"
        },
        outputName: {
          label: "输出名",
          hint: "作为最终 GIF 文件名前缀。"
        },
        width: {
          label: "宽度",
          hint: "留空则保留源视频宽度。"
        },
        height: {
          label: "高度",
          hint: "留空则保留源视频高度。"
        },
        loop: {
          label: "循环次数",
          hint: "0 表示无限循环。"
        },
        reverse: {
          label: "倒放",
          hint: "按反向顺序输出帧。"
        },
        fitMode: {
          label: "适配模式",
          hint: "Contain 会补边，Cover 会裁切铺满。"
        },
        background: {
          label: "补边背景色",
          hint: "当 contain 需要补边时使用。"
        },
        maxColors: {
          label: "最大颜色数",
          hint: "GIF 调色板最多支持 256 色。"
        },
        scaleAlgorithm: {
          label: "缩放算法",
          hint: "Lanczos 更锐利，Bilinear 更轻。"
        },
        dither: {
          label: "抖动算法",
          hint: "影响纹理感和渐变过渡。"
        },
        paletteStatsMode: {
          label: "调色板采样",
          hint: "控制 ffmpeg 如何为调色板取样。"
        },
        diffMode: {
          label: "差分模式",
          hint: "用于变化区域的优化。"
        }
      },
      options: {
        fitMode: {
          contain: "完整显示",
          cover: "铺满裁切",
          stretch: "强制拉伸",
          original: "保持比例"
        },
        dither: {
          sierra2_4a: "Sierra 2-4A",
          floyd_steinberg: "Floyd-Steinberg",
          sierra2: "Sierra 2",
          bayer: "Bayer",
          heckbert: "Heckbert",
          none: "无"
        },
        paletteStatsMode: {
          full: "全帧",
          diff: "帧差",
          single: "单帧"
        },
        diffMode: {
          rectangle: "矩形差分",
          none: "不做差分优化"
        },
        scaleAlgorithm: {
          lanczos: "Lanczos",
          bicubic: "Bicubic",
          bilinear: "Bilinear",
          spline: "Spline",
          neighbor: "Nearest"
        }
      }
    },
    result: {
      sectionNote: "生成后的 GIF 会保留 24 小时，到期后自动失效并清理。",
      empty: "提交任务后可在这里查看本次 ffmpeg 命令。",
      job: "任务"
    },
    library: {
      title: "有效 GIF",
      empty: "当前浏览器还没有有效的 GIF。",
      expiresIn: "剩余",
      expired: "已过期"
    },
    messages: {
      selectFile: "请先选择一个视频文件。",
      ffmpegMissing: "渲染前请先确保 ffmpeg 可用，或保留本地 .tools 版本。",
      deleteFailed: "暂时无法删除这个 GIF。"
    }
  }
};

function resolveInitialTheme() {
  return localStorage.getItem("vtg-theme") || document.documentElement.dataset.theme || "light";
}

function resolveInitialParamState() {
  try {
    const rawParams = localStorage.getItem(PARAMS_STORAGE_KEY);
    const rawMeta = localStorage.getItem(PARAMS_META_STORAGE_KEY);
    const storedParams = rawParams ? JSON.parse(rawParams) : {};
    const storedMeta = rawMeta ? JSON.parse(rawMeta) : {};

    const mergedParams = {
      ...initialParams,
      ...storedParams
    };

    // Migrate the old implicit width default so "blank means keep source size"
    // starts working for existing users after refresh.
    if (mergedParams.width === "480" && storedMeta.widthExplicit !== true) {
      mergedParams.width = "";
    }

    return {
      params: {
        ...mergedParams
      },
      outputNameManual: Boolean(storedMeta.outputNameManual)
    };
  } catch {
    return {
      params: { ...initialParams },
      outputNameManual: false
    };
  }
}

function resolveInitialLocale() {
  const stored = localStorage.getItem("vtg-locale");
  if (stored === "zh" || stored === "en") {
    return stored;
  }

  return navigator.language.toLowerCase().startsWith("zh") ? "zh" : "en";
}

function resolveInitialGIFHistory() {
  try {
    const rawHistory = localStorage.getItem(GIF_HISTORY_STORAGE_KEY);
    if (!rawHistory) {
      return [];
    }

    const parsed = JSON.parse(rawHistory);
    if (!Array.isArray(parsed)) {
      return [];
    }

    return parsed.filter((item) => isGIFActive(item));
  } catch {
    return [];
  }
}

function App() {
  const fileInputRef = useRef(null);
  const dragDepthRef = useRef(0);
  const [paramState, setParamState] = useState(resolveInitialParamState);
  const [gifLibrary, setGifLibrary] = useState(resolveInitialGIFHistory);
  const [locale, setLocale] = useState(resolveInitialLocale);
  const [theme, setTheme] = useState(resolveInitialTheme);
  const [dragging, setDragging] = useState(false);
  const [file, setFile] = useState(null);
  const [videoPreviewUrl, setVideoPreviewUrl] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [result, setResult] = useState(null);
  const [health, setHealth] = useState({ available: true, ffmpeg: "" });
  const [now, setNow] = useState(Date.now());
  const [deletingGIFs, setDeletingGIFs] = useState([]);

  const { params, outputNameManual } = paramState;
  const copy = dictionaries[locale];
  const downloadExpiresAt = result?.expiresAt ? Date.parse(result.expiresAt) : 0;
  const resultExpired = Boolean(result && downloadExpiresAt && now >= downloadExpiresAt);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("vtg-theme", theme);
  }, [theme]);

  useEffect(() => {
    document.documentElement.lang = locale === "zh" ? "zh-CN" : "en";
    localStorage.setItem("vtg-locale", locale);
  }, [locale]);

  useEffect(() => {
    localStorage.setItem(PARAMS_STORAGE_KEY, JSON.stringify(params));
    localStorage.setItem(PARAMS_META_STORAGE_KEY, JSON.stringify({
      outputNameManual,
      widthExplicit: params.width !== ""
    }));
  }, [params, outputNameManual]);

  useEffect(() => {
    localStorage.setItem(GIF_HISTORY_STORAGE_KEY, JSON.stringify(gifLibrary));
  }, [gifLibrary]);

  useEffect(() => {
    if (!result?.expiresAt && gifLibrary.length === 0) {
      return undefined;
    }

    setNow(Date.now());
    const timer = window.setInterval(() => {
      setNow(Date.now());
    }, 1000);

    return () => {
      window.clearInterval(timer);
    };
  }, [gifLibrary.length, result?.expiresAt]);

  useEffect(() => {
    setGifLibrary((current) => {
      const next = current.filter((item) => isGIFActive(item, now));
      return next.length === current.length ? current : next;
    });
  }, [now]);

  useEffect(() => {
    let active = true;
    fetch("/api/health")
      .then((response) => response.json())
      .then((payload) => {
        if (active) {
          setHealth(payload);
        }
      })
      .catch(() => {
        if (active) {
          setHealth({ available: false, ffmpeg: "" });
        }
      });

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!file) {
      setVideoPreviewUrl("");
      return undefined;
    }

    const nextUrl = URL.createObjectURL(file);
    setVideoPreviewUrl(nextUrl);

    return () => {
      URL.revokeObjectURL(nextUrl);
    };
  }, [file]);

  useEffect(() => {
    const handleDragEnter = (event) => {
      if (!isFileDrag(event)) {
        return;
      }

      event.preventDefault();
      dragDepthRef.current += 1;
      setDragging(true);
    };

    const handleDragOver = (event) => {
      if (!isFileDrag(event)) {
        return;
      }

      event.preventDefault();
      if (event.dataTransfer) {
        event.dataTransfer.dropEffect = "copy";
      }
      setDragging(true);
    };

    const handleDragLeave = (event) => {
      if (!isFileDrag(event)) {
        return;
      }

      event.preventDefault();
      dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
      if (dragDepthRef.current === 0) {
        setDragging(false);
      }
    };

    const handleDropWindow = (event) => {
      if (!isFileDrag(event)) {
        return;
      }

      event.preventDefault();
      dragDepthRef.current = 0;
      setDragging(false);
      const [nextFile] = event.dataTransfer?.files || [];
      assignFile(nextFile);
    };

    window.addEventListener("dragenter", handleDragEnter);
    window.addEventListener("dragover", handleDragOver);
    window.addEventListener("dragleave", handleDragLeave);
    window.addEventListener("drop", handleDropWindow);

    return () => {
      window.removeEventListener("dragenter", handleDragEnter);
      window.removeEventListener("dragover", handleDragOver);
      window.removeEventListener("dragleave", handleDragLeave);
      window.removeEventListener("drop", handleDropWindow);
    };
  }, []);

  const fileMeta = useMemo(() => {
    if (!file) {
      return [];
    }

    return [
      file.name,
      humanizeBytes(file.size),
      file.type || copy.upload.fallbackType
    ];
  }, [copy.upload.fallbackType, file]);

  const activeGIFLibrary = useMemo(
    () => gifLibrary.filter((item) => isGIFActive(item, now)),
    [gifLibrary, now]
  );

  const toggleTheme = () => {
    setTheme((current) => (current === "dark" ? "light" : "dark"));
  };

  const updateField = (id, value, options = {}) => {
    setParamState((current) => ({
      params: {
        ...current.params,
        [id]: value
      },
      outputNameManual: id === "outputName" ? (options.manual ?? true) : current.outputNameManual
    }));
  };

  const assignFile = (nextFile) => {
    if (!nextFile) {
      return;
    }

    setFile(nextFile);
    setError("");
    setResult(null);

    const baseName = nextFile.name.replace(/\.[^.]+$/, "").toLowerCase().replace(/[^a-z0-9-_]+/g, "-");
    if (baseName) {
      setParamState((current) => ({
        params: {
          ...current.params,
          outputName: current.outputNameManual ? current.params.outputName : baseName
        },
        outputNameManual: current.outputNameManual
      }));
    }
  };

  const handleFileChange = (event) => {
    const [nextFile] = event.target.files || [];
    assignFile(nextFile);
  };

  const handleSubmit = async (event) => {
    event.preventDefault();
    if (!file) {
      setError(copy.messages.selectFile);
      return;
    }

    setLoading(true);
    setError("");
    setResult(null);

    const formData = new FormData();
    formData.append("video", file);

    Object.entries(params).forEach(([key, value]) => {
      formData.append(key, typeof value === "boolean" ? String(value) : value);
    });

    try {
      const response = await fetch("/api/convert", {
        method: "POST",
        body: formData
      });

      const payload = await response.json();
      if (!response.ok) {
        throw new Error(payload.error || "GIF rendering failed");
      }

      setNow(Date.now());
      setResult(payload);
      setGifLibrary((current) => [
        normalizeGIFRecord(payload),
        ...current.filter((item) => item.jobId !== payload.jobId && item.downloadName !== payload.downloadName)
      ]);
    } catch (submitError) {
      setError(submitError.message);
    } finally {
      setLoading(false);
    }
  };

  const handleDeleteGIF = async (gif) => {
    setDeletingGIFs((current) => [...current, gif.downloadName]);
    setError("");

    try {
      const response = await fetch(`/api/gifs/${encodeURIComponent(gif.downloadName)}`, {
        method: "DELETE"
      });

      if (!response.ok) {
        throw new Error(copy.messages.deleteFailed);
      }

      setGifLibrary((current) => current.filter((item) => item.downloadName !== gif.downloadName));
      if (result?.downloadName === gif.downloadName) {
        setResult(null);
      }
    } catch (deleteError) {
      setError(deleteError.message || copy.messages.deleteFailed);
    } finally {
      setDeletingGIFs((current) => current.filter((name) => name !== gif.downloadName));
    }
  };

  const handleReset = () => {
    localStorage.removeItem(PARAMS_STORAGE_KEY);
    localStorage.removeItem(PARAMS_META_STORAGE_KEY);
    setParamState({
      params: { ...initialParams },
      outputNameManual: false
    });
    setFile(null);
    setResult(null);
    setError("");
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  };

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brandmark">
          <div className="brandmark-icon" aria-hidden="true">
            <span />
            <span />
            <span />
          </div>
          <div className="brand-copy">
            <span>{copy.brand.caption}</span>
            <strong>{copy.brand.title}</strong>
          </div>
        </div>

        <div className="topbar-tools">
          <div className="language-switch" role="group" aria-label={copy.languageLabel}>
            <button
              type="button"
              className={`lang-button ${locale === "zh" ? "is-active" : ""}`}
              onClick={() => setLocale("zh")}
            >
              中文
            </button>
            <button
              type="button"
              className={`lang-button ${locale === "en" ? "is-active" : ""}`}
              onClick={() => setLocale("en")}
            >
              EN
            </button>
          </div>

          <span className={`status-pill ${health.available ? "is-live" : "is-error"}`}>
            {health.available ? copy.status.ready : copy.status.missing}
          </span>

          <button type="button" className="theme-switch" onClick={toggleTheme} aria-label="Toggle theme">
            <span>{theme === "dark" ? copy.actions.light : copy.actions.dark}</span>
          </button>
        </div>
      </header>

      <main className="workspace">
        <aside className="stage">
          <div className="stage-sticky">
            <section
              className={`dropzone ${dragging ? "is-dragging" : ""} ${file ? "has-file" : ""}`}
              onClick={() => fileInputRef.current?.click()}
            >
              <input
                ref={fileInputRef}
                type="file"
                accept="video/*"
                className="sr-only"
                onChange={handleFileChange}
              />

              <div className="dropzone-copy">
                <p className="eyebrow">{copy.upload.kicker}</p>
                <span className="dropzone-label">{file ? copy.upload.titleLoaded : copy.upload.titleIdle}</span>
                <p>{file ? copy.upload.bodyLoaded : copy.upload.bodyIdle}</p>
              </div>

              <div className="file-strip">
                {fileMeta.length > 0 ? (
                  fileMeta.map((item) => (
                    <span key={item} className="meta-chip">
                      {item}
                    </span>
                  ))
                ) : (
                  copy.upload.defaultChips.map((item) => (
                    <span key={item} className="meta-chip">
                      {item}
                    </span>
                  ))
                )}
              </div>
            </section>

            <div className="media-stack">
              <section className="media-panel">
                <div className="panel-head">
                  <span>{copy.preview.sourceTitle}</span>
                  <span>{file ? copy.preview.sourceReady : copy.preview.sourceWaiting}</span>
                </div>
                <div className="preview-frame">
                  {videoPreviewUrl ? (
                    <video src={videoPreviewUrl} controls muted playsInline />
                  ) : (
                    <div className="placeholder-state">
                      <p>{copy.preview.sourceEmpty}</p>
                    </div>
                  )}
                </div>
              </section>

              <section className="media-panel">
                <div className="panel-head">
                  <span>{copy.preview.resultTitle}</span>
                  <span>
                    {result
                      ? resultExpired
                        ? copy.preview.resultEmptyState
                        : humanizeBytes(result.sizeBytes)
                      : copy.preview.resultEmptyState}
                  </span>
                </div>
                <div className="preview-frame">
                  {result && !resultExpired ? (
                    <img src={result.outputUrl} alt={copy.preview.resultAlt} />
                  ) : (
                    <div className="placeholder-state">
                      <p>{resultExpired ? copy.preview.resultExpired : copy.preview.resultEmpty}</p>
                    </div>
                  )}
                </div>
                {result && !resultExpired ? (
                  <div className="result-actions">
                    <a
                      href={result.downloadUrl || result.outputUrl}
                      download={result.downloadName}
                      className="primary-link"
                    >
                      {copy.actions.download}
                    </a>
                    <div className="result-meta">
                      <span className="meta-chip">{copy.preview.resultWindow}</span>
                      {result.expiresAt ? (
                        <span className="meta-chip">
                          {copy.preview.expiresAt} {formatExpiry(result.expiresAt, locale)}
                        </span>
                      ) : null}
                    </div>
                  </div>
                ) : null}
              </section>

              <section className="media-panel">
                <div className="panel-head">
                  <span>{copy.library.title}</span>
                  <span>{activeGIFLibrary.length}</span>
                </div>
                <div className="gif-library">
                  {activeGIFLibrary.length > 0 ? (
                    activeGIFLibrary.map((gif) => {
                      const deleting = deletingGIFs.includes(gif.downloadName);
                      return (
                        <article key={gif.jobId || gif.downloadName} className="gif-card">
                          <div className="gif-card-preview">
                            <img src={gif.outputUrl} alt={gif.downloadName} />
                          </div>
                          <div className="gif-card-body">
                            <strong className="gif-card-name">{gif.downloadName}</strong>
                            <div className="gif-card-meta">
                              <span className="meta-chip">{humanizeBytes(gif.sizeBytes)}</span>
                              <span className="meta-chip">
                                {copy.library.expiresIn} {formatCountdown(gif.expiresAt, now, locale, copy.library.expired)}
                              </span>
                            </div>
                            <div className="gif-card-actions">
                              <a href={gif.downloadUrl || gif.outputUrl} download={gif.downloadName} className="secondary-button">
                                {copy.actions.download}
                              </a>
                              <button
                                type="button"
                                className="secondary-button is-danger"
                                onClick={() => handleDeleteGIF(gif)}
                                disabled={deleting}
                              >
                                {deleting ? copy.actions.deleting : copy.actions.delete}
                              </button>
                            </div>
                          </div>
                        </article>
                      );
                    })
                  ) : (
                    <div className="placeholder-state is-compact">
                      <p>{copy.library.empty}</p>
                    </div>
                  )}
                </div>
              </section>
            </div>
          </div>
        </aside>

        <form className="inspector" onSubmit={handleSubmit}>
          <div className="inspector-head">
            <div>
              <p className="eyebrow">{copy.form.kicker}</p>
              <strong className="inspector-title">{copy.form.title}</strong>
            </div>
            <div className="head-actions">
              <button type="button" className="secondary-button" onClick={handleReset}>
                {copy.actions.reset}
              </button>
              <button type="submit" className="primary-button" disabled={!file || loading || !health.available}>
                {loading ? copy.actions.rendering : copy.actions.render}
              </button>
            </div>
          </div>

          <div className="message-stack">
            {error ? <div className="message-strip is-error">{error}</div> : null}
            {!health.available ? (
              <div className="message-strip is-warning">{copy.messages.ffmpegMissing}</div>
            ) : null}
          </div>

          <section className="section-block">
            <div className="section-title">
              <h3>{copy.form.sections.timing}</h3>
            </div>
            <div className="field-grid">
              {timingFieldDefs.map((field) => (
                <InputField
                  key={field.id}
                  field={{ ...field, ...copy.form.fields[field.id] }}
                  value={params[field.id]}
                  onChange={(value) => updateField(field.id, value, { manual: field.id === "outputName" })}
                />
              ))}
              <ToggleField
                label={copy.form.fields.reverse.label}
                checked={params.reverse}
                hint={copy.form.fields.reverse.hint}
                onChange={(checked) => updateField("reverse", checked)}
              />
            </div>
          </section>

          <section className="section-block">
            <div className="section-title">
              <h3>{copy.form.sections.output}</h3>
            </div>
            <div className="field-grid">
              {outputFieldDefs.map((field) => (
                <InputField
                  key={field.id}
                  field={{ ...field, ...copy.form.fields[field.id] }}
                  value={params[field.id]}
                  onChange={(value) => updateField(field.id, value)}
                />
              ))}
              <SelectField
                label={copy.form.fields.fitMode.label}
                value={params.fitMode}
                options={mapOptions(copy.form.options.fitMode, ["contain", "cover", "stretch", "original"])}
                hint={copy.form.fields.fitMode.hint}
                onChange={(value) => updateField("fitMode", value)}
              />
              <ColorField
                label={copy.form.fields.background.label}
                value={params.background}
                hint={copy.form.fields.background.hint}
                onChange={(value) => updateField("background", value)}
              />
            </div>
          </section>

          <section className="section-block">
            <div className="section-title">
              <h3>{copy.form.sections.quality}</h3>
            </div>
            <div className="field-grid">
              <InputField
                field={{
                  id: "maxColors",
                  type: "number",
                  step: "1",
                  min: "2",
                  max: "256",
                  ...copy.form.fields.maxColors
                }}
                value={params.maxColors}
                onChange={(value) => updateField("maxColors", value)}
              />
              <SelectField
                label={copy.form.fields.scaleAlgorithm.label}
                value={params.scaleAlgorithm}
                options={mapOptions(copy.form.options.scaleAlgorithm, ["lanczos", "bicubic", "bilinear", "spline", "neighbor"])}
                hint={copy.form.fields.scaleAlgorithm.hint}
                onChange={(value) => updateField("scaleAlgorithm", value)}
              />
              <SelectField
                label={copy.form.fields.dither.label}
                value={params.dither}
                options={mapOptions(copy.form.options.dither, ["sierra2_4a", "floyd_steinberg", "sierra2", "bayer", "heckbert", "none"])}
                hint={copy.form.fields.dither.hint}
                onChange={(value) => updateField("dither", value)}
              />
              <SelectField
                label={copy.form.fields.paletteStatsMode.label}
                value={params.paletteStatsMode}
                options={mapOptions(copy.form.options.paletteStatsMode, ["full", "diff", "single"])}
                hint={copy.form.fields.paletteStatsMode.hint}
                onChange={(value) => updateField("paletteStatsMode", value)}
              />
              <SelectField
                label={copy.form.fields.diffMode.label}
                value={params.diffMode}
                options={mapOptions(copy.form.options.diffMode, ["rectangle", "none"])}
                hint={copy.form.fields.diffMode.hint}
                onChange={(value) => updateField("diffMode", value)}
              />
            </div>
          </section>

        </form>
      </main>

      {dragging ? (
        <div className="global-drop-overlay" aria-hidden="true">
          <div className="global-drop-card">
            <span className="meta-chip">{copy.upload.kicker}</span>
            <strong>{copy.upload.overlay}</strong>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function InputField({ field, value, onChange }) {
  return (
    <label className="field">
      <span className="field-label">{field.label}</span>
      <input
        type={field.type}
        value={value}
        min={field.min}
        max={field.max}
        step={field.step}
        placeholder={field.placeholder}
        onChange={(event) => onChange(event.target.value)}
      />
      <span className="field-hint">{field.hint}</span>
    </label>
  );
}

function SelectField({ label, value, options, hint, onChange }) {
  return (
    <label className="field">
      <span className="field-label">{label}</span>
      <select value={value} onChange={(event) => onChange(event.target.value)}>
        {options.map(([optionValue, optionLabel]) => (
          <option key={optionValue} value={optionValue}>
            {optionLabel}
          </option>
        ))}
      </select>
      <span className="field-hint">{hint}</span>
    </label>
  );
}

function ColorField({ label, value, hint, onChange }) {
  const colorValue = /^#[0-9a-fA-F]{6}$/.test(value) ? value : "#0f172a";

  return (
    <label className="field">
      <span className="field-label">{label}</span>
      <div className="color-input">
        <input type="color" value={colorValue} onChange={(event) => onChange(event.target.value)} />
        <input type="text" value={value} onChange={(event) => onChange(event.target.value)} />
      </div>
      <span className="field-hint">{hint}</span>
    </label>
  );
}

function ToggleField({ label, checked, hint, onChange }) {
  return (
    <label className="toggle-row">
      <div>
        <span className="field-label">{label}</span>
        <span className="field-hint">{hint}</span>
      </div>
      <button
        type="button"
        className={`toggle-button ${checked ? "is-active" : ""}`}
        onClick={() => onChange(!checked)}
        aria-pressed={checked}
      >
        <span />
      </button>
    </label>
  );
}

function mapOptions(dictionary, order) {
  return order.map((value) => [value, dictionary[value]]);
}

function normalizeGIFRecord(payload) {
  return {
    jobId: payload.jobId,
    outputUrl: payload.outputUrl,
    downloadUrl: payload.downloadUrl,
    downloadName: payload.downloadName,
    sizeBytes: payload.sizeBytes,
    expiresAt: payload.expiresAt
  };
}

function isFileDrag(event) {
  return Array.from(event.dataTransfer?.types || []).includes("Files");
}

function isGIFActive(gif, referenceTime = Date.now()) {
  if (!gif?.downloadName || !gif?.outputUrl || !gif?.expiresAt) {
    return false;
  }

  const expiresAt = Date.parse(gif.expiresAt);
  if (Number.isNaN(expiresAt)) {
    return false;
  }

  return expiresAt > referenceTime;
}

function formatExpiry(value, locale) {
  const date = new Date(value);
  const formatter = new Intl.DateTimeFormat(locale === "zh" ? "zh-CN" : "en-US", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit"
  });

  return formatter.format(date);
}

function formatCountdown(value, referenceTime, locale, expiredLabel) {
  const remainingMs = Date.parse(value) - referenceTime;
  if (remainingMs <= 0) {
    return expiredLabel;
  }

  const totalSeconds = Math.floor(remainingMs / 1000);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;

  if (hours > 0) {
    return locale === "zh"
      ? `${hours}时 ${minutes}分`
      : `${hours}h ${minutes}m`;
  }

  return locale === "zh"
    ? `${minutes}分 ${seconds}秒`
    : `${minutes}m ${seconds}s`;
}

function humanizeBytes(value) {
  if (!value && value !== 0) {
    return "";
  }
  if (value < 1024) {
    return `${value} B`;
  }
  const units = ["KB", "MB", "GB"];
  let size = value / 1024;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }
  return `${size.toFixed(size >= 10 ? 0 : 1)} ${units[unitIndex]}`;
}

export default App;
