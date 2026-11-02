export namespace dbx {
	
	export class Column {
	    name: string;
	    type: string;
	    nullable: boolean;
	    pk: boolean;
	    comment: string;
	    unique: boolean;
	    fk: string;
	    default: string;
	    extra: string;
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.nullable = source["nullable"];
	        this.pk = source["pk"];
	        this.comment = source["comment"];
	        this.unique = source["unique"];
	        this.fk = source["fk"];
	        this.default = source["default"];
	        this.extra = source["extra"];
	    }
	}
	export class ColumnAlter {
	    old: Column;
	    new: Column;
	
	    static createFrom(source: any = {}) {
	        return new ColumnAlter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.old = this.convertValues(source["old"], Column);
	        this.new = this.convertValues(source["new"], Column);
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
	export class Table {
	    schema: string;
	    name: string;
	    type: string;
	    comment: string;
	    rows: number;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new Table(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.comment = source["comment"];
	        this.rows = source["rows"];
	        this.size = source["size"];
	    }
	}
	export class AlterRequest {
	    connId: string;
	    schema: string;
	    table: Table;
	    changes: ColumnAlter[];
	
	    static createFrom(source: any = {}) {
	        return new AlterRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connId = source["connId"];
	        this.schema = source["schema"];
	        this.table = this.convertValues(source["table"], Table);
	        this.changes = this.convertValues(source["changes"], ColumnAlter);
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
	export class AlterResult {
	    applied: number;
	    total: number;
	    error: string;
	    rolledBack: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AlterResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.applied = source["applied"];
	        this.total = source["total"];
	        this.error = source["error"];
	        this.rolledBack = source["rolledBack"];
	    }
	}
	export class RowChange {
	    kind: string;
	    key: Record<string, any>;
	    values: Record<string, any>;
	
	    static createFrom(source: any = {}) {
	        return new RowChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.key = source["key"];
	        this.values = source["values"];
	    }
	}
	export class ApplyRequest {
	    connId: string;
	    schema: string;
	    table: Table;
	    changes: RowChange[];
	
	    static createFrom(source: any = {}) {
	        return new ApplyRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connId = source["connId"];
	        this.schema = source["schema"];
	        this.table = this.convertValues(source["table"], Table);
	        this.changes = this.convertValues(source["changes"], RowChange);
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
	
	
	export class SchemaFilter {
	    all: boolean;
	    current: boolean;
	    names: string[];
	    patterns: string[];
	
	    static createFrom(source: any = {}) {
	        return new SchemaFilter(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.all = source["all"];
	        this.current = source["current"];
	        this.names = source["names"];
	        this.patterns = source["patterns"];
	    }
	}
	export class ConnConfig {
	    id: string;
	    name: string;
	    driver: string;
	    host: string;
	    port: number;
	    user: string;
	    password?: string;
	    savePassword: boolean;
	    database: string;
	    oracleSid: boolean;
	    params: string;
	    color: string;
	    ssh: boolean;
	    sshHost: string;
	    sshPort: number;
	    sshUser: string;
	    sshAuth: string;
	    sshPassword?: string;
	    sshKeyFile: string;
	    sshHostKey: string;
	    schemas?: SchemaFilter;
	
	    static createFrom(source: any = {}) {
	        return new ConnConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.driver = source["driver"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.password = source["password"];
	        this.savePassword = source["savePassword"];
	        this.database = source["database"];
	        this.oracleSid = source["oracleSid"];
	        this.params = source["params"];
	        this.color = source["color"];
	        this.ssh = source["ssh"];
	        this.sshHost = source["sshHost"];
	        this.sshPort = source["sshPort"];
	        this.sshUser = source["sshUser"];
	        this.sshAuth = source["sshAuth"];
	        this.sshPassword = source["sshPassword"];
	        this.sshKeyFile = source["sshKeyFile"];
	        this.sshHostKey = source["sshHostKey"];
	        this.schemas = this.convertValues(source["schemas"], SchemaFilter);
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
	export class EditInfo {
	    editable: boolean;
	    insertable: boolean;
	    reason: string;
	    schema: string;
	    table: Table;
	    columns: string[];
	    key: string[];
	    keys: string[];
	    refs: string[];
	    comments: string[];
	
	    static createFrom(source: any = {}) {
	        return new EditInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.editable = source["editable"];
	        this.insertable = source["insertable"];
	        this.reason = source["reason"];
	        this.schema = source["schema"];
	        this.table = this.convertValues(source["table"], Table);
	        this.columns = source["columns"];
	        this.key = source["key"];
	        this.keys = source["keys"];
	        this.refs = source["refs"];
	        this.comments = source["comments"];
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
	export class ExecRequest {
	    tabId: string;
	    connId: string;
	    schema: string;
	    sql: string;
	    cursor: number;
	    fetch: number;
	
	    static createFrom(source: any = {}) {
	        return new ExecRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.tabId = source["tabId"];
	        this.connId = source["connId"];
	        this.schema = source["schema"];
	        this.sql = source["sql"];
	        this.cursor = source["cursor"];
	        this.fetch = source["fetch"];
	    }
	}
	export class ResultColumn {
	    name: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new ResultColumn(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	    }
	}
	export class StmtResult {
	    sql: string;
	    from: number;
	    to: number;
	    columns: ResultColumn[];
	    rows: any[][];
	    hasMore: boolean;
	    cursorId: string;
	    paged: boolean;
	    rowsAffected: number;
	    elapsedMs: number;
	    error: string;
	    notice: string;
	    source: string[];
	
	    static createFrom(source: any = {}) {
	        return new StmtResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sql = source["sql"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.columns = this.convertValues(source["columns"], ResultColumn);
	        this.rows = source["rows"];
	        this.hasMore = source["hasMore"];
	        this.cursorId = source["cursorId"];
	        this.paged = source["paged"];
	        this.rowsAffected = source["rowsAffected"];
	        this.elapsedMs = source["elapsedMs"];
	        this.error = source["error"];
	        this.notice = source["notice"];
	        this.source = source["source"];
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
	export class ExecResponse {
	    results: StmtResult[];
	    currentSchema: string;
	
	    static createFrom(source: any = {}) {
	        return new ExecResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.results = this.convertValues(source["results"], StmtResult);
	        this.currentSchema = source["currentSchema"];
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
	export class FetchResponse {
	    rows: any[][];
	    hasMore: boolean;
	
	    static createFrom(source: any = {}) {
	        return new FetchResponse(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rows = source["rows"];
	        this.hasMore = source["hasMore"];
	    }
	}
	
	export class Routine {
	    schema: string;
	    name: string;
	    type: string;
	    args: string;
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new Routine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.type = source["type"];
	        this.args = source["args"];
	        this.id = source["id"];
	    }
	}
	
	
	export class SchemaList {
	    schemas: string[];
	    current: string;
	
	    static createFrom(source: any = {}) {
	        return new SchemaList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schemas = source["schemas"];
	        this.current = source["current"];
	    }
	}
	

}

export namespace main {
	
	export class OpenedFile {
	    name: string;
	    content: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenedFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.content = source["content"];
	    }
	}
	export class TestResult {
	    version: string;
	    sshHostKey: string;
	
	    static createFrom(source: any = {}) {
	        return new TestResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.sshHostKey = source["sshHostKey"];
	    }
	}

}

