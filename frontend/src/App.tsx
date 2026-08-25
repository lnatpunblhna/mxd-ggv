import {useCallback, useEffect, useRef, useState, type KeyboardEvent, type MouseEvent} from 'react';
import './App.css';
import {
    ListWindows,
    StartCapture,
    StopCapture,
    NextFrame,
    UpdateOptions,
    StartPetFeed,
    StopPetFeed,
    PetFeedStatus,
    IsElevated,
    RestartAsAdmin,
    CalibratePotions,
    StartPotionWatch,
    StopPotionWatch,
    PotionStatus,
    LoadPotionCalibration,
} from '../wailsjs/go/main/App';
import {EventsOn} from '../wailsjs/runtime/runtime';
import {petfeed, potion, vision} from '../wailsjs/go/models';

type SelMode = 'hpSlot' | 'mpSlot' | 'hpBar' | 'mpBar' | null;
type Rel = {x: number; y: number; w: number; h: number};

const emptyRel: Rel = {x: 0, y: 0, w: 0, h: 0};

function relValid(r?: Rel | null): boolean {
    return !!r && r.w >= 0.004 && r.h >= 0.004;
}

function asRel(r?: Rel | potion.RelRect | null): Rel {
    if (!r) return emptyRel;
    return {x: r.x || 0, y: r.y || 0, w: r.w || 0, h: r.h || 0};
}

const SEL_LABEL: Record<Exclude<SelMode, null>, string> = {
    hpSlot: '血药格',
    mpSlot: '蓝药格',
    hpBar: '血条',
    mpBar: '蓝条',
};

const SEL_COLOR: Record<Exclude<SelMode, null>, string> = {
    hpSlot: '#f85149',
    mpSlot: '#58a6ff',
    hpBar: '#ff7b72',
    mpBar: '#79c0ff',
};

const ZOOM_MIN = 1;
const ZOOM_MAX = 8;
const SEL_ORDER: Exclude<SelMode, null>[] = ['hpSlot', 'mpSlot', 'hpBar', 'mpBar'];

function clamp(n: number, lo: number, hi: number) {
    return Math.min(hi, Math.max(lo, n));
}

function contentBox(img: HTMLImageElement) {
    const r = img.getBoundingClientRect();
    const nw = img.naturalWidth;
    const nh = img.naturalHeight;
    if (!nw || !nh) return null;
    const scale = Math.min(r.width / nw, r.height / nh);
    const w = nw * scale;
    const h = nh * scale;
    return {left: r.left + (r.width - w) / 2, top: r.top + (r.height - h) / 2, w, h};
}

function contentBoxLocal(img: HTMLImageElement) {
    const nw = img.naturalWidth;
    const nh = img.naturalHeight;
    if (!nw || !nh) return null;
    const cw = img.offsetWidth;
    const ch = img.offsetHeight;
    if (!cw || !ch) return null;
    const scale = Math.min(cw / nw, ch / nh);
    const w = nw * scale;
    const h = nh * scale;
    return {left: img.offsetLeft + (cw - w) / 2, top: img.offsetTop + (ch - h) / 2, w, h};
}

function countLabel(n: number) {
    if (n == null || n < 0) return '--';
    return String(n);
}

function stateLabel(s?: string) {
    switch (s) {
        case 'ok': return '充足';
        case 'low': return '偏低';
        case 'empty': return '已空';
        case 'unknown': return '未知';
        default: return '未校准';
    }
}

function playAlertSound(kind: string, level: string) {
    const Ctx = window.AudioContext || (window as any).webkitAudioContext;
    if (!Ctx) return;
    const ctx = new Ctx();
    const freq = kind === 'hp' ? 880 : 540;
    const times = level === 'empty' ? [0, 0.16] : [0];
    times.forEach(delay => {
        const osc = ctx.createOscillator();
        const gain = ctx.createGain();
        osc.type = 'square';
        osc.frequency.value = freq;
        gain.gain.value = 0.07;
        osc.connect(gain);
        gain.connect(ctx.destination);
        const t0 = ctx.currentTime + delay;
        osc.start(t0);
        gain.gain.exponentialRampToValueAtTime(0.001, t0 + 0.12);
        osc.stop(t0 + 0.13);
    });
}

const sleep = (ms: number) => new Promise(r => setTimeout(r, ms));

const MODIFIER_CODES = new Set([
    'ShiftLeft', 'ShiftRight', 'ControlLeft', 'ControlRight',
    'AltLeft', 'AltRight', 'MetaLeft', 'MetaRight',
]);

function hotkeyLabel(code: string): string {
    if (!code) return '';
    if (/^Digit[0-9]$/.test(code)) return code.slice(5);
    if (/^Numpad[0-9]$/.test(code)) return 'Num' + code.slice(6);
    if (/^Key[A-Za-z]$/.test(code)) return code.slice(3).toUpperCase();
    return code;
}

function remainLabel(nextDecay: number, now: number): string {
    if (!nextDecay) return '--';
    const ms = nextDecay - now;
    if (ms <= 0) return '0:00';
    const total = Math.ceil(ms / 1000);
    const m = Math.floor(total / 60);
    const s = total % 60;
    return `${m}:${s.toString().padStart(2, '0')}`;
}

function barClass(fullness: number): string {
    if (fullness < 40) return 'fullness-bar crit';
    if (fullness < 70) return 'fullness-bar low';
    return 'fullness-bar';
}

function App() {
    const [windows, setWindows] = useState<vision.WindowInfo[]>([]);
    const [handle, setHandle] = useState<number>(0);
    const [running, setRunning] = useState(false);
    const [frame, setFrame] = useState<vision.FramePayload | null>(null);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);

    const [fps, setFps] = useState(8);
    const [quality, setQuality] = useState(70);
    const [maxWidth, setMaxWidth] = useState(960);

    const [fullnessInput, setFullnessInput] = useState('');
    const [hotkey, setHotkey] = useState('');
    const [hotkeyFocus, setHotkeyFocus] = useState(false);
    const [feed, setFeed] = useState<petfeed.Status | null>(null);
    const [now, setNow] = useState(() => Date.now());
    const [feedBusy, setFeedBusy] = useState(false);
    const [elevated, setElevated] = useState<boolean | null>(null);

    const [selMode, setSelMode] = useState<SelMode>(null);
    const [picking, setPicking] = useState(false);
    const [drag, setDrag] = useState<Rel | null>(null);
    const dragOrigin = useRef<{x: number; y: number} | null>(null);
    const [view, setView] = useState({zoom: 1, x: 0, y: 0});
    const [panning, setPanning] = useState(false);
    const viewRef = useRef(view);
    const panDrag = useRef<{x: number; y: number; vx: number; vy: number} | null>(null);
    const viewportRef = useRef<HTMLDivElement | null>(null);
    const [hpSlot, setHpSlot] = useState<Rel>(emptyRel);
    const [mpSlot, setMpSlot] = useState<Rel>(emptyRel);
    const [hpBar, setHpBar] = useState<Rel>(emptyRel);
    const [mpBar, setMpBar] = useState<Rel>(emptyRel);
    const [hpCount, setHpCount] = useState(-1);
    const [mpCount, setMpCount] = useState(-1);
    const [lowCount, setLowCount] = useState(10);
    const [potionSt, setPotionSt] = useState<potion.Status | null>(null);
    const [potionBusy, setPotionBusy] = useState(false);
    const [hpThumb, setHpThumb] = useState('');
    const [mpThumb, setMpThumb] = useState('');
    const [overlayBox, setOverlayBox] = useState<{left: number; top: number; w: number; h: number} | null>(null);

    const loopId = useRef(0);
    const imgRef = useRef<HTMLImageElement | null>(null);
    const stageRef = useRef<HTMLElement | null>(null);
    const feeding = !!feed?.enabled;
    const watching = !!potionSt?.enabled;
    viewRef.current = view;
    const hiRes = picking || view.zoom > 1.25;

    const refresh = useCallback(async () => {
        setBusy(true);
        try {
            const list = await ListWindows();
            setWindows(list || []);
            setError('');
            setHandle(prev => {
                if (prev && (list || []).some(w => w.handle === prev)) return prev;
                const game = (list || []).find(w => w.isGame);
                return game ? game.handle : (list && list.length ? list[0].handle : 0);
            });
        } catch (e: any) {
            setError(String(e));
        } finally {
            setBusy(false);
        }
    }, []);

    useEffect(() => {
        refresh();
    }, [refresh]);

    useEffect(() => {
        if (!running) return;
        UpdateOptions(makeOptions()).catch(e => setError(String(e)));
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [fps, quality, maxWidth, running, hiRes]);

    function makeOptions(): vision.Options {
        return {
            fps,
            quality: hiRes ? Math.max(quality, 80) : quality,
            maxWidth: hiRes ? Math.max(maxWidth, 1600) : maxWidth,
        } as vision.Options;
    }

    async function start() {
        if (!handle) {
            setError('请先选择一个窗口');
            return;
        }
        setBusy(true);
        try {
            await StartCapture(handle, makeOptions());
            setError('');
            setRunning(true);
        } catch (e: any) {
            setError(String(e));
        } finally {
            setBusy(false);
        }
    }

    async function stop() {
        setRunning(false);
        if (picking) exitPick();
        try {
            await StopCapture();
        } catch (e: any) {
            setError(String(e));
        }
    }

    useEffect(() => {
        if (!running) return;
        const id = ++loopId.current;
        let seq = 0;
        (async () => {
            while (loopId.current === id) {
                try {
                    const f = await NextFrame(seq);
                    if (loopId.current !== id) break;
                    if (f.error) setError(f.error); else setError('');
                    if (f.data) {
                        seq = f.seq;
                        setFrame(f);
                    } else {
                        await sleep(30);
                    }
                } catch (e: any) {
                    if (loopId.current !== id) break;
                    setError(String(e));
                    await sleep(500);
                }
            }
        })();
        return () => {
            loopId.current++;
        };
    }, [running]);

    useEffect(() => {
        IsElevated().then(setElevated).catch(() => setElevated(false));
        PetFeedStatus().then(setFeed).catch(() => {});
        const offFeed = EventsOn('petfeed:status', (st: petfeed.Status) => {
            setFeed(st);
            if (st?.enabled || st?.startedAt) {
                setFullnessInput(String(st.fullness));
                if (st.hotkey) setHotkey(st.hotkey);
            }
        });
        PotionStatus().then(applyPotionStatus).catch(() => {});
        LoadPotionCalibration().then(applyCalibView).catch(() => {});
        const offPotion = EventsOn('potion:status', applyPotionStatus);
        const offAlert = EventsOn('potion:alert', (al: potion.Alert) => {
            playAlertSound(al?.kind, al?.level);
        });
        return () => { offFeed(); offPotion(); offAlert(); };
    }, []);

    useEffect(() => {
        if (!feeding) return;
        const id = window.setInterval(() => setNow(Date.now()), 1000);
        setNow(Date.now());
        return () => window.clearInterval(id);
    }, [feeding]);

    async function startFeed() {
        if (!handle) {
            setError('请先选择窗口');
            return;
        }
        const raw = fullnessInput.trim();
        if (raw === '') {
            setError('请填写当前饱满感');
            return;
        }
        const n = Number(raw);
        if (!Number.isInteger(n) || n < 0 || n > 100) {
            setError('饱满感应为 0–100 的整数');
            return;
        }
        if (!hotkey) {
            setError('请填写喂食快捷键');
            return;
        }
        setFeedBusy(true);
        try {
            await StartPetFeed(handle, n, hotkey);
            setError('');
        } catch (e: any) {
            setError(String(e));
        } finally {
            setFeedBusy(false);
        }
    }

    async function restartAdmin() {
        setFeedBusy(true);
        try {
            await RestartAsAdmin();
        } catch (e: any) {
            setError(String(e));
        } finally {
            setFeedBusy(false);
        }
    }

    async function stopFeed() {
        setFeedBusy(true);
        try {
            await StopPetFeed();
            setError('');
        } catch (e: any) {
            setError(String(e));
        } finally {
            setFeedBusy(false);
        }
    }

    function applyCalibView(v: potion.CalibrationView) {
        if (!v) return;
        if (relValid(v.hpSlot)) setHpSlot(asRel(v.hpSlot));
        if (relValid(v.mpSlot)) setMpSlot(asRel(v.mpSlot));
        if (relValid(v.hpBar)) setHpBar(asRel(v.hpBar));
        if (relValid(v.mpBar)) setMpBar(asRel(v.mpBar));
        if (v.hpPreview) setHpThumb(v.hpPreview);
        if (v.mpPreview) setMpThumb(v.mpPreview);
        if ((v.hpCount ?? 0) > 0) setHpCount(v.hpCount);
        if ((v.mpCount ?? 0) > 0) setMpCount(v.mpCount);
    }

    // 数量只由后端识别，前端不接受输入，识别不出来时保留上一次的读数。
    function applyPotionStatus(st: potion.Status) {
        setPotionSt(st);
        if (!st) return;
        if ((st.hp?.count ?? -1) >= 0) setHpCount(st.hp.count);
        if ((st.mp?.count ?? -1) >= 0) setMpCount(st.mp.count);
    }

    function syncOverlay() {
        const img = imgRef.current;
        if (!img) {
            setOverlayBox(null);
            return;
        }
        const box = contentBoxLocal(img);
        if (!box) {
            setOverlayBox(null);
            return;
        }
        setOverlayBox(box);
    }

    useEffect(() => {
        const id = requestAnimationFrame(syncOverlay);
        return () => cancelAnimationFrame(id);
    }, [frame, running, picking]);

    useEffect(() => {
        const stage = stageRef.current;
        if (!stage || typeof ResizeObserver === 'undefined') return;
        const ro = new ResizeObserver(() => syncOverlay());
        ro.observe(stage);
        return () => ro.disconnect();
    }, []);

    function resetView() {
        setView({zoom: 1, x: 0, y: 0});
    }

    function exitPick() {
        setPicking(false);
        setSelMode(null);
        setDrag(null);
        dragOrigin.current = null;
        panDrag.current = null;
        setPanning(false);
        resetView();
    }

    function enterPick(m: Exclude<SelMode, null>) {
        if (!picking) resetView();
        setPicking(true);
        setSelMode(selMode === m ? null : m);
        setDrag(null);
        dragOrigin.current = null;
    }

    function applyZoomAt(next: number, cx: number, cy: number) {
        const {zoom, x, y} = viewRef.current;
        const z = clamp(next, ZOOM_MIN, ZOOM_MAX);
        if (Math.abs(z - zoom) < 0.001) return;
        if (z <= ZOOM_MIN + 0.001) {
            setView({zoom: 1, x: 0, y: 0});
            return;
        }
        const wx = (cx - x) / zoom;
        const wy = (cy - y) / zoom;
        setView({zoom: z, x: cx - wx * z, y: cy - wy * z});
    }

    useEffect(() => {
        const onKey = (e: globalThis.KeyboardEvent) => {
            const tag = (e.target as HTMLElement | null)?.tagName;
            const typing = tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA';
            if (e.key === 'Escape') {
                if (selMode || dragOrigin.current) {
                    setSelMode(null);
                    setDrag(null);
                    dragOrigin.current = null;
                    return;
                }
                if (picking) {
                    exitPick();
                }
                return;
            }
            if (typing || !frame?.data) return;
            if (e.key === '0') resetView();
            if (e.key === '=' || e.key === '+') applyZoomAt(viewRef.current.zoom * 1.25, 0, 0);
            if (e.key === '-' || e.key === '_') applyZoomAt(viewRef.current.zoom / 1.25, 0, 0);
        };
        window.addEventListener('keydown', onKey);
        window.addEventListener('resize', syncOverlay);
        return () => {
            window.removeEventListener('keydown', onKey);
            window.removeEventListener('resize', syncOverlay);
        };
    }, [picking, selMode, frame]);

    useEffect(() => {
        const stage = stageRef.current;
        if (!stage) return;
        const onWheel = (e: WheelEvent) => {
            if (!imgRef.current) return;
            e.preventDefault();
            const r = stage.getBoundingClientRect();
            const cx = e.clientX - r.left - r.width / 2;
            const cy = e.clientY - r.top - r.height / 2;
            const factor = e.deltaY < 0 ? 1.15 : 1 / 1.15;
            applyZoomAt(viewRef.current.zoom * factor, cx, cy);
        };
        stage.addEventListener('wheel', onWheel, {passive: false});
        return () => stage.removeEventListener('wheel', onWheel);
    }, []);

    function toRatio(e: {clientX: number; clientY: number}) {
        const img = imgRef.current;
        if (!img) return null;
        const box = contentBox(img);
        if (!box || box.w < 1 || box.h < 1) return null;
        const x = Math.min(1, Math.max(0, (e.clientX - box.left) / box.w));
        const y = Math.min(1, Math.max(0, (e.clientY - box.top) / box.h));
        return {x, y};
    }

    function rectFromOrigin(p: {x: number; y: number}): Rel {
        const o = dragOrigin.current || p;
        return {
            x: Math.min(o.x, p.x),
            y: Math.min(o.y, p.y),
            w: Math.abs(p.x - o.x),
            h: Math.abs(p.y - o.y),
        };
    }

    function commitRect(r: Rel | null, mode: SelMode) {
        if (!mode) return;
        if (!r || !relValid(r)) {
            setSelMode(null);
            return;
        }
        switch (mode) {
            case 'hpSlot': setHpSlot(r); break;
            case 'mpSlot': setMpSlot(r); break;
            case 'hpBar': setHpBar(r); break;
            case 'mpBar': setMpBar(r); break;
        }
        const filled: Record<Exclude<SelMode, null>, boolean> = {
            hpSlot: mode === 'hpSlot' || relValid(hpSlot),
            mpSlot: mode === 'mpSlot' || relValid(mpSlot),
            hpBar: mode === 'hpBar' || relValid(hpBar),
            mpBar: mode === 'mpBar' || relValid(mpBar),
        };
        const next = SEL_ORDER.find(m => m !== mode && !filled[m]) || null;
        setSelMode(next);
    }

    function endPan() {
        if (!panDrag.current) return;
        panDrag.current = null;
        setPanning(false);
    }

    function onPreviewDown(e: MouseEvent<HTMLElement>) {
        if (e.button === 2 || e.button === 1 || (e.button === 0 && !selMode && viewRef.current.zoom > 1)) {
            e.preventDefault();
            panDrag.current = {
                x: e.clientX,
                y: e.clientY,
                vx: viewRef.current.x,
                vy: viewRef.current.y,
            };
            setPanning(true);
            const onMove = (ev: globalThis.MouseEvent) => {
                const d = panDrag.current;
                if (!d) return;
                setView({zoom: viewRef.current.zoom, x: d.vx + (ev.clientX - d.x), y: d.vy + (ev.clientY - d.y)});
            };
            const onUp = () => {
                window.removeEventListener('mousemove', onMove);
                window.removeEventListener('mouseup', onUp);
                endPan();
            };
            window.addEventListener('mousemove', onMove);
            window.addEventListener('mouseup', onUp);
            return;
        }
        if (e.button !== 0 || !selMode || watching) return;
        const p = toRatio(e);
        if (!p) return;
        e.preventDefault();
        const mode = selMode;
        dragOrigin.current = p;
        setDrag({x: p.x, y: p.y, w: 0, h: 0});
        const onMove = (ev: globalThis.MouseEvent) => {
            const q = toRatio(ev);
            if (!q) return;
            setDrag(rectFromOrigin(q));
        };
        const onUp = (ev: globalThis.MouseEvent) => {
            window.removeEventListener('mousemove', onMove);
            window.removeEventListener('mouseup', onUp);
            const q = toRatio(ev);
            const r = q ? rectFromOrigin(q) : null;
            dragOrigin.current = null;
            setDrag(null);
            commitRect(r, mode);
        };
        window.addEventListener('mousemove', onMove);
        window.addEventListener('mouseup', onUp);
    }

    // calibrate 只做一件事：按当前框选截一帧，自动读出画面里的血药蓝药数量。
    async function calibrate() {
        if (!handle) {
            setError('请先选择窗口');
            return;
        }
        const hasSlot = relValid(hpSlot) || relValid(mpSlot);
        if (!hasSlot && !relValid(hpBar) && !relValid(mpBar)) {
            setError('还没框选。请先点上面的「血药格 / 蓝药格」，在画面里拖框选出药格再校准');
            return;
        }
        setPotionBusy(true);
        try {
            const view = await CalibratePotions(handle, {hpSlot, mpSlot, hpBar, mpBar} as potion.CalibSpec);
            applyCalibView(view);
            applyPotionStatus(await PotionStatus());
            const missHP = relValid(hpSlot) && !((view?.hpCount ?? 0) > 0);
            const missMP = relValid(mpSlot) && !((view?.mpCount ?? 0) > 0);
            if (missHP || missMP) {
                const who = [missHP ? '血药' : '', missMP ? '蓝药' : ''].filter(Boolean).join('、');
                setError(`没认出${who}数量。把药格框紧一点（只框住图标和数字）再校准一次`);
            } else {
                setError('');
            }
        } catch (e: any) {
            setError(String(e));
        } finally {
            setPotionBusy(false);
        }
    }

    async function startWatch() {
        if (!handle) {
            setError('请先选择窗口');
            return;
        }
        setPotionBusy(true);
        try {
            await StartPotionWatch(handle, {
                lowCount,
                emptyFrames: 6,
                cooldownSec: 180,
                barLow: 0.4,
                barStuckFrames: 6,
                intervalMS: 500,
            } as potion.WatchOptions);
            setError('');
        } catch (e: any) {
            setError(String(e));
        } finally {
            setPotionBusy(false);
        }
    }

    async function stopWatch() {
        setPotionBusy(true);
        try {
            await StopPotionWatch();
            setError('');
        } catch (e: any) {
            setError(String(e));
        } finally {
            setPotionBusy(false);
        }
    }

    function onHotkeyDown(e: KeyboardEvent<HTMLInputElement>) {
        e.preventDefault();
        e.stopPropagation();
        if (feeding) return;
        if (MODIFIER_CODES.has(e.code)) return;
        setHotkey(e.code);
    }

    const selected = windows.find(w => w.handle === handle);
    const typedFullness = fullnessInput.trim() === '' ? NaN : Number(fullnessInput);
    const liveFullness = feeding ? feed!.fullness : typedFullness;
    const barWidth = Number.isFinite(liveFullness) ? Math.max(0, Math.min(100, liveFullness)) : 0;

    const zoomPct = Math.round(view.zoom * 100);
    const slotOf = (m: Exclude<SelMode, null>) =>
        m === 'hpSlot' ? hpSlot : m === 'mpSlot' ? mpSlot : m === 'hpBar' ? hpBar : mpBar;

    return (
        <div className={'app' + (picking ? ' picking' : '')}>
            <header className="bar">
                <select
                    className="win-select"
                    value={handle || ''}
                    onChange={e => setHandle(Number(e.target.value))}
                    disabled={running}
                >
                    {windows.length === 0 && <option value="">未找到窗口</option>}
                    {windows.map(w => (
                        <option key={w.handle} value={w.handle}>
                            {w.isGame ? '🎮 ' : ''}{w.title || '(无标题)'} — {w.width}×{w.height} [{w.process}]
                        </option>
                    ))}
                </select>
                <button className="btn" onClick={refresh} disabled={busy || running}>刷新</button>
                {running
                    ? <button className="btn danger" onClick={stop}>■ 停止</button>
                    : <button className="btn primary" onClick={start} disabled={busy || !handle}>▶ 开始预览</button>}
                <span className={'dot ' + (running ? 'on' : 'off')} title={running ? '捕捉中' : '已停止'}/>
            </header>

            <main
                className={'stage' + (picking ? ' picking' : '') + (selMode ? ' selecting' : '') + (panning ? ' panning' : '')}
                ref={el => { stageRef.current = el; }}
                onMouseDown={onPreviewDown}
                onDoubleClick={() => resetView()}
                onContextMenu={e => e.preventDefault()}
            >
                {frame?.data
                    ? (
                        <div
                            ref={viewportRef}
                            className="preview-viewport"
                            style={{transform: `translate(${view.x}px, ${view.y}px) scale(${view.zoom})`}}
                        >
                            <img
                                ref={imgRef}
                                className={'preview' + (view.zoom >= 2 ? ' crisp' : '')}
                                src={'data:image/jpeg;base64,' + frame.data}
                                alt="实时画面"
                                draggable={false}
                                onLoad={syncOverlay}
                            />
                            {overlayBox && (
                                <div
                                    className="roi-layer"
                                    style={{left: overlayBox.left, top: overlayBox.top, width: overlayBox.w, height: overlayBox.h}}
                                >
                                    <RoiBox r={hpSlot} color={SEL_COLOR.hpSlot} label="血药"/>
                                    <RoiBox r={mpSlot} color={SEL_COLOR.mpSlot} label="蓝药"/>
                                    <RoiBox r={hpBar} color={SEL_COLOR.hpBar} label="血条"/>
                                    <RoiBox r={mpBar} color={SEL_COLOR.mpBar} label="蓝条"/>
                                    {drag && selMode && <RoiBox r={drag} color={SEL_COLOR[selMode]} label={SEL_LABEL[selMode]} live/>}
                                    <PotionDanmaku watching={watching} hp={potionSt?.hp} mp={potionSt?.mp}/>
                                </div>
                            )}
                        </div>
                    )
                    : <div className="placeholder">
                        {running ? '等待第一帧…' : '选择冒险岛窗口后点击「开始预览」（游戏窗口需可见）'}
                    </div>}
                {!picking && view.zoom > 1 && (
                    <div className="zoom-badge" onMouseDown={e => e.stopPropagation()}>
                        {zoomPct}%
                        <button className="btn" onClick={resetView}>复位</button>
                    </div>
                )}
                {picking && (
                    <div
                        className="pick-bar"
                        onMouseDown={e => e.stopPropagation()}
                        onMouseUp={e => e.stopPropagation()}
                        onDoubleClick={e => e.stopPropagation()}
                    >
                        {SEL_ORDER.map(m => (
                            <button
                                key={m}
                                className={'btn pick' + (selMode === m ? ' active' : '') + (relValid(slotOf(m)) ? ' set' : '')}
                                disabled={watching}
                                onClick={() => enterPick(m)}
                            >{SEL_LABEL[m]}</button>
                        ))}
                        <span className="pick-hint">
                            {selMode ? `拖拽框选${SEL_LABEL[selMode]}` : '点上方按钮选择区域'}
                            {' · '}滚轮缩放 {zoomPct}% · 右键平移 · 双击复位
                        </span>
                        {view.zoom > 1 && <button className="btn" onClick={resetView}>复位</button>}
                        <button className="btn primary" onClick={exitPick}>完成</button>
                    </div>
                )}
            </main>

            {error && <div className="error">{error}</div>}

            <footer className="controls">
                <label className="ctl">
                    <span>帧率 <b>{fps}</b></span>
                    <input type="range" min={1} max={30} value={fps} onChange={e => setFps(+e.target.value)}/>
                </label>
                <label className="ctl">
                    <span>画质 <b>{quality}</b></span>
                    <input type="range" min={20} max={95} value={quality} onChange={e => setQuality(+e.target.value)}/>
                </label>
                <label className="ctl">
                    <span>预览宽度 <b>{maxWidth}</b></span>
                    <input type="range" min={320} max={1920} step={40} value={maxWidth}
                           onChange={e => setMaxWidth(+e.target.value)}/>
                </label>
            </footer>

            <div className="panels">
            <section className="pet-panel">
                <div className="pet-head">
                    <span className="pet-title">宠物自动喂食</span>
                    <span className="pet-hint">每 1 分钟 -1，低于 70 自动喂食 +30；发键时会切到游戏窗口</span>
                </div>
                <div className="pet-row">
                    <label className="ctl">
                        <span>当前饱满感</span>
                        <input
                            type="number"
                            min={0}
                            max={100}
                            step={1}
                            value={fullnessInput}
                            disabled={feeding}
                            placeholder="0–100"
                            onChange={e => setFullnessInput(e.target.value)}
                        />
                    </label>
                    <label className="ctl">
                        <span>喂食快捷键</span>
                        <input
                            className="hotkey"
                            type="text"
                            readOnly
                            value={hotkeyFocus && !hotkey ? '' : hotkeyLabel(hotkey)}
                            disabled={feeding}
                            placeholder={hotkeyFocus ? '按下按键…' : '点击后按下快捷键'}
                            onFocus={() => setHotkeyFocus(true)}
                            onBlur={() => setHotkeyFocus(false)}
                            onKeyDown={onHotkeyDown}
                        />
                    </label>
                    {feeding
                        ? <button className="btn danger" onClick={stopFeed} disabled={feedBusy}>关闭</button>
                        : <button className="btn primary" onClick={startFeed} disabled={feedBusy || !handle}>开启</button>}
                    <span className={'dot ' + (feeding ? 'on' : 'off')} title={feeding ? '喂食中' : '未开启'}/>
                </div>
                {elevated === false && (
                    <div className="pet-warn">
                        <span>当前不是管理员权限。冒险岛若以管理员运行，发键会被系统拒绝。</span>
                        <button className="btn" onClick={restartAdmin} disabled={feedBusy}>以管理员身份重启</button>
                    </div>
                )}
                <div className="pet-meter">
                    <span>{Number.isFinite(liveFullness) ? liveFullness : '--'}/100</span>
                    <div className={barClass(Number.isFinite(liveFullness) ? liveFullness : 0)}>
                        <i style={{width: barWidth + '%'}}/>
                    </div>
                    <span>下次 -1 {feeding ? remainLabel(feed?.nextDecay || 0, now) : '--'}</span>
                    <span>已喂食 {feed?.feedCount ?? 0} 次</span>
                </div>
                {feed?.lastError && <div className="pet-error">{feed.lastError}</div>}
            </section>

            <section className="pet-panel potion-panel">
                <div className="pet-head">
                    <span className="pet-title">血药蓝药提醒</span>
                    <span className="pet-hint">先框出血药格和蓝药格，再点「校准」自动读出画面里的数量。数量由程序识别，不用手填。</span>
                </div>
                <div className="pet-row">
                    {(['hpSlot', 'mpSlot', 'hpBar', 'mpBar'] as Exclude<SelMode, null>[]).map(m => (
                        <button
                            key={m}
                            className={'btn pick' + (selMode === m ? ' active' : '') + (relValid(m === 'hpSlot' ? hpSlot : m === 'mpSlot' ? mpSlot : m === 'hpBar' ? hpBar : mpBar) ? ' set' : '')}
                            disabled={watching || !running}
                            onClick={() => enterPick(m)}
                        >{SEL_LABEL[m]}</button>
                    ))}
                </div>
                <div className="pet-row">
                    <label className="ctl">
                        <span>血药当前数量</span>
                        <input className="readonly" type="text" readOnly tabIndex={-1} value={countLabel(hpCount)}/>
                    </label>
                    <label className="ctl">
                        <span>蓝药当前数量</span>
                        <input className="readonly" type="text" readOnly tabIndex={-1} value={countLabel(mpCount)}/>
                    </label>
                    <label className="ctl">
                        <span>低量阈值</span>
                        <input
                            type="number"
                            min={0}
                            max={999}
                            step={1}
                            value={lowCount}
                            disabled={watching}
                            onChange={e => setLowCount(Math.max(0, Number(e.target.value) || 0))}
                        />
                    </label>
                    <button
                        className="btn"
                        onClick={calibrate}
                        disabled={potionBusy || watching || !handle}
                        title="按当前框选自动读出画面里的血药蓝药数量"
                    >校准</button>
                    {watching
                        ? <button className="btn danger" onClick={stopWatch} disabled={potionBusy}>关闭</button>
                        : <button className="btn primary" onClick={startWatch} disabled={potionBusy || !handle}>开启</button>}
                    <span className={'dot ' + (watching ? 'on' : 'off')} title={watching ? '监测中' : '未开启'}/>
                </div>
                <div className="potion-thumbs">
                    {hpThumb && <img src={'data:image/jpeg;base64,' + hpThumb} alt="血药格" title="血药格模板"/>}
                    {mpThumb && <img src={'data:image/jpeg;base64,' + mpThumb} alt="蓝药格" title="蓝药格模板"/>}
                    <PotionMeter kind="血药" slot={potionSt?.hp} watching={watching}/>
                    <PotionMeter kind="蓝药" slot={potionSt?.mp} watching={watching}/>
                </div>
                {potionSt?.lastAlert && watching && (potionSt.hp?.state === 'empty' || potionSt.mp?.state === 'empty' || potionSt.hp?.state === 'low' || potionSt.mp?.state === 'low') && (
                    <div className={'potion-alert' + ((potionSt.hp?.state === 'empty' || potionSt.mp?.state === 'empty') ? ' empty' : '')}>
                        {potionSt.hp?.state === 'empty' ? '血药已空' : potionSt.hp?.state === 'low' ? '血药偏低' : ''}
                        {(potionSt.hp?.state === 'empty' || potionSt.hp?.state === 'low') && (potionSt.mp?.state === 'empty' || potionSt.mp?.state === 'low') ? ' · ' : ''}
                        {potionSt.mp?.state === 'empty' ? '蓝药已空' : potionSt.mp?.state === 'low' ? '蓝药偏低' : ''}
                    </div>
                )}
                {potionSt?.lastError && <div className="pet-error">{potionSt.lastError}</div>}
            </section>
            </div>

            <div className="status">
                {selected && <span>{selected.title}</span>}
                {frame && <>
                    <span>源 {frame.srcWidth}×{frame.srcHeight}</span>
                    <span>预览 {frame.width}×{frame.height}</span>
                    <span>{frame.fps.toFixed(1)} fps</span>
                    <span>{frame.captureMS.toFixed(1)} ms</span>
                    <span>{frame.method}</span>
                </>}
            </div>
        </div>
    );
}

type DanmakuItem = {
    id: number;
    text: string;
    kind: string;
    level: string;
    top: number;
    duration: number;
};

function danmakuMessages(hpState?: string, hpCount?: number, mpState?: string, mpCount?: number): {text: string; kind: string; level: string}[] {
    const out: {text: string; kind: string; level: string}[] = [];
    const add = (kind: 'hp' | 'mp', state?: string, count?: number) => {
        if (state !== 'low' && state !== 'empty') return;
        const name = kind === 'hp' ? '血药' : '蓝药';
        const n = count ?? -1;
        const text = state === 'empty'
            ? name + '已空  快补药！'
            : n >= 0
                ? `${name}不足  剩余 ${n}`
                : name + '不足  请及时补给';
        out.push({text, kind, level: state});
    };
    add('hp', hpState, hpCount);
    add('mp', mpState, mpCount);
    return out;
}

function PotionDanmaku({watching, hp, mp}: {watching: boolean; hp?: potion.SlotStatus; mp?: potion.SlotStatus}) {
    const [items, setItems] = useState<DanmakuItem[]>([]);
    const idRef = useRef(0);
    const hpState = hp?.state;
    const mpState = mp?.state;
    const hpCount = hp?.count;
    const mpCount = mp?.count;

    useEffect(() => {
        if (!watching) {
            setItems([]);
            return;
        }
        const spawn = () => {
            const msgs = danmakuMessages(hpState, hpCount, mpState, mpCount);
            if (!msgs.length) return;
            const msg = msgs[idRef.current % msgs.length];
            const id = ++idRef.current;
            setItems(prev => [...prev.slice(-16), {
                id,
                text: msg.text,
                kind: msg.kind,
                level: msg.level,
                top: 8 + (id % 5) * 14,
                duration: 4.6 + (id % 3) * 0.7,
            }]);
        };
        spawn();
        const empty = hpState === 'empty' || mpState === 'empty';
        const timer = window.setInterval(spawn, empty ? 700 : 1100);
        return () => window.clearInterval(timer);
    }, [watching, hpState, hpCount, mpState, mpCount]);

    if (!watching || items.length === 0) return null;
    return (
        <div className="danmaku-layer" aria-hidden="true">
            {items.map(it => (
                <span
                    key={it.id}
                    className={'danmaku ' + it.kind + ' ' + it.level}
                    style={{top: it.top + '%', animationDuration: it.duration + 's'}}
                    onAnimationEnd={() => setItems(prev => prev.filter(x => x.id !== it.id))}
                >{it.text}</span>
            ))}
        </div>
    );
}

function RoiBox({r, color, label, live}: {r: Rel; color: string; label: string; live?: boolean}) {
    if (!relValid(r) && !live) return null;
    if (r.w <= 0 || r.h <= 0) return null;
    return (
        <div
            className={'roi' + (live ? ' live' : '')}
            style={{
                left: r.x * 100 + '%',
                top: r.y * 100 + '%',
                width: r.w * 100 + '%',
                height: r.h * 100 + '%',
                borderColor: color,
                background: color + '33',
            }}
        >
            <span>{label}</span>
        </div>
    );
}

function PotionMeter({kind, slot, watching}: {kind: string; slot?: potion.SlotStatus; watching: boolean}) {
    const raw = slot?.state || '';
    const calibrated = raw !== '' && raw !== 'absent';
    const state = watching ? (raw || 'unknown') : (calibrated ? (raw || 'unknown') : 'absent');
    const count = slot?.count ?? -1;
    const bar = slot?.bar ?? -1;
    const barPct = bar >= 0 ? Math.max(0, Math.min(100, bar * 100)) : 0;
    const stateText = watching ? stateLabel(raw || 'unknown') : (calibrated ? '已校准' : '未校准');
    return (
        <div className={'potion-meter ' + state}>
            <b>{kind}</b>
            <span className="potion-state">{stateText}</span>
            <span>数量 {countLabel(count)}</span>
            {bar >= 0 && (
                <span className="potion-bar" style={{color: kind === '血药' ? '#f85149' : '#58a6ff'}} title={'血蓝条 ' + barPct.toFixed(0) + '%'}>
                    <i style={{width: barPct + '%'}}/>
                </span>
            )}
        </div>
    );
}

export default App
