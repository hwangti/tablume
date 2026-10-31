export namespace dbx {
	
	export class Column {
	    name: string;
	    type: string;
	    nullable: boolean;
	    pk: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Column(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.type = source["type"];
	        this.nullable = source["nullable"];
	        this.pk = source["pk"];
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
	    rowsAffected: number;
	    elapsedMs: number;
	    error: string;
	    notice: string;
	
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
	        this.rowsAffected = source["rowsAffected"];
	        this.elapsedMs = source["elapsedMs"];
	        this.error = source["error"];
	        this.notice = source["notice"];
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
	
	export class Table {
	    schema: string;
	    name: string;
	    type: string;
	
	    static createFrom(source: any = {}) {
	        return new Table(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.schema = source["schema"];
	        this.name = source["name"];
	        this.type = source["type"];
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

}

