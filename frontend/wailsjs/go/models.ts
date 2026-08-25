export namespace petfeed {
	
	export class Status {
	    enabled: boolean;
	    fullness: number;
	    hotkey: string;
	    handle: number;
	    feedCount: number;
	    nextDecay: number;
	    lastFeed: number;
	    lastError: string;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.fullness = source["fullness"];
	        this.hotkey = source["hotkey"];
	        this.handle = source["handle"];
	        this.feedCount = source["feedCount"];
	        this.nextDecay = source["nextDecay"];
	        this.lastFeed = source["lastFeed"];
	        this.lastError = source["lastError"];
	        this.startedAt = source["startedAt"];
	    }
	}

}

export namespace potion {
	
	export class Alert {
	    kind: string;
	    level: string;
	    reason: string;
	    count: number;
	    at: number;
	
	    static createFrom(source: any = {}) {
	        return new Alert(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.level = source["level"];
	        this.reason = source["reason"];
	        this.count = source["count"];
	        this.at = source["at"];
	    }
	}
	export class RelRect {
	    x: number;
	    y: number;
	    w: number;
	    h: number;
	
	    static createFrom(source: any = {}) {
	        return new RelRect(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.w = source["w"];
	        this.h = source["h"];
	    }
	}
	export class CalibSpec {
	    hpSlot: RelRect;
	    mpSlot: RelRect;
	    hpBar: RelRect;
	    mpBar: RelRect;
	
	    static createFrom(source: any = {}) {
	        return new CalibSpec(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hpSlot = this.convertValues(source["hpSlot"], RelRect);
	        this.mpSlot = this.convertValues(source["mpSlot"], RelRect);
	        this.hpBar = this.convertValues(source["hpBar"], RelRect);
	        this.mpBar = this.convertValues(source["mpBar"], RelRect);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CalibrationView {
	    hpSlot: RelRect;
	    mpSlot: RelRect;
	    hpBar: RelRect;
	    mpBar: RelRect;
	    hasHPSlot: boolean;
	    hasMPSlot: boolean;
	    hasHPBar: boolean;
	    hasMPBar: boolean;
	    frameW: number;
	    frameH: number;
	    hpPreview: string;
	    mpPreview: string;
	    hpCount: number;
	    mpCount: number;
	
	    static createFrom(source: any = {}) {
	        return new CalibrationView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hpSlot = this.convertValues(source["hpSlot"], RelRect);
	        this.mpSlot = this.convertValues(source["mpSlot"], RelRect);
	        this.hpBar = this.convertValues(source["hpBar"], RelRect);
	        this.mpBar = this.convertValues(source["mpBar"], RelRect);
	        this.hasHPSlot = source["hasHPSlot"];
	        this.hasMPSlot = source["hasMPSlot"];
	        this.hasHPBar = source["hasHPBar"];
	        this.hasMPBar = source["hasMPBar"];
	        this.frameW = source["frameW"];
	        this.frameH = source["frameH"];
	        this.hpPreview = source["hpPreview"];
	        this.mpPreview = source["mpPreview"];
	        this.hpCount = source["hpCount"];
	        this.mpCount = source["mpCount"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class SlotStatus {
	    state: string;
	    count: number;
	    ncc: number;
	    bar: number;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new SlotStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.count = source["count"];
	        this.ncc = source["ncc"];
	        this.bar = source["bar"];
	        this.reason = source["reason"];
	    }
	}
	export class Status {
	    enabled: boolean;
	    handle: number;
	    calibrated: boolean;
	    startedAt: number;
	    lastError: string;
	    hp: SlotStatus;
	    mp: SlotStatus;
	    lastAlert?: Alert;
	    hasHPSlot: boolean;
	    hasMPSlot: boolean;
	    hasHPBar: boolean;
	    hasMPBar: boolean;
	    hpSlot: RelRect;
	    mpSlot: RelRect;
	    hpBar: RelRect;
	    mpBar: RelRect;
	    lowCount: number;
	    emptyFrames: number;
	    cooldownSec: number;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.handle = source["handle"];
	        this.calibrated = source["calibrated"];
	        this.startedAt = source["startedAt"];
	        this.lastError = source["lastError"];
	        this.hp = this.convertValues(source["hp"], SlotStatus);
	        this.mp = this.convertValues(source["mp"], SlotStatus);
	        this.lastAlert = this.convertValues(source["lastAlert"], Alert);
	        this.hasHPSlot = source["hasHPSlot"];
	        this.hasMPSlot = source["hasMPSlot"];
	        this.hasHPBar = source["hasHPBar"];
	        this.hasMPBar = source["hasMPBar"];
	        this.hpSlot = this.convertValues(source["hpSlot"], RelRect);
	        this.mpSlot = this.convertValues(source["mpSlot"], RelRect);
	        this.hpBar = this.convertValues(source["hpBar"], RelRect);
	        this.mpBar = this.convertValues(source["mpBar"], RelRect);
	        this.lowCount = source["lowCount"];
	        this.emptyFrames = source["emptyFrames"];
	        this.cooldownSec = source["cooldownSec"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class WatchOptions {
	    lowCount: number;
	    emptyFrames: number;
	    cooldownSec: number;
	    barLow: number;
	    barStuckFrames: number;
	    intervalMS: number;
	
	    static createFrom(source: any = {}) {
	        return new WatchOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.lowCount = source["lowCount"];
	        this.emptyFrames = source["emptyFrames"];
	        this.cooldownSec = source["cooldownSec"];
	        this.barLow = source["barLow"];
	        this.barStuckFrames = source["barStuckFrames"];
	        this.intervalMS = source["intervalMS"];
	    }
	}

}

export namespace vision {
	
	export class FramePayload {
	    seq: number;
	    data: string;
	    width: number;
	    height: number;
	    srcWidth: number;
	    srcHeight: number;
	    fps: number;
	    captureMS: number;
	    method: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new FramePayload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.seq = source["seq"];
	        this.data = source["data"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.srcWidth = source["srcWidth"];
	        this.srcHeight = source["srcHeight"];
	        this.fps = source["fps"];
	        this.captureMS = source["captureMS"];
	        this.method = source["method"];
	        this.error = source["error"];
	    }
	}
	export class Options {
	    fps: number;
	    quality: number;
	    maxWidth: number;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fps = source["fps"];
	        this.quality = source["quality"];
	        this.maxWidth = source["maxWidth"];
	    }
	}
	export class WindowInfo {
	    pid: number;
	    handle: number;
	    title: string;
	    process: string;
	    width: number;
	    height: number;
	    isGame: boolean;
	    hidden: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WindowInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.pid = source["pid"];
	        this.handle = source["handle"];
	        this.title = source["title"];
	        this.process = source["process"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.isGame = source["isGame"];
	        this.hidden = source["hidden"];
	    }
	}

}

