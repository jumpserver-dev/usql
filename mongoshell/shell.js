// Mongo shell helpers run in Goja. All database I/O uses the official Go driver.
"use strict";

function ObjectId(value) {
  if (!(this instanceof ObjectId)) return new ObjectId(value);
  this.str = value === undefined ? __objectID() : String(value);
  if (!/^[a-fA-F0-9]{24}$/.test(this.str)) throw new TypeError("ObjectId requires 24 hexadecimal characters");
  this.str = this.str.toLowerCase();
}
ObjectId.prototype.toHexString = function () { return this.str; };
ObjectId.prototype.toString = function () { return this.str; };
ObjectId.prototype.equals = function (other) { return other instanceof ObjectId && this.str === other.str; };
ObjectId.prototype.getTimestamp = function () { return new Date(parseInt(this.str.slice(0, 8), 16) * 1000); };
ObjectId.prototype.toJSON = function () { return {$oid: this.str}; };

function NumberLong(value) {
  if (!(this instanceof NumberLong)) return new NumberLong(value);
  this.value = String(value === undefined ? 0 : value);
  if (!/^-?\d+$/.test(this.value)) throw new TypeError("NumberLong requires an integer; use a string for large values");
}
NumberLong.prototype.toString = function () { return this.value; };
NumberLong.prototype.valueOf = function () { return Number(this.value); };
NumberLong.prototype.toJSON = function () { return {$numberLong: this.value}; };
var Long = NumberLong;

function NumberInt(value) {
  if (!(this instanceof NumberInt)) return new NumberInt(value);
  this.value = Number(value === undefined ? 0 : value) | 0;
}
NumberInt.prototype.valueOf = function () { return this.value; };
NumberInt.prototype.toString = function () { return String(this.value); };
NumberInt.prototype.toJSON = function () { return {$numberInt: String(this.value)}; };
var Int32 = NumberInt;
function NumberDecimal(value) {
  if (!(this instanceof NumberDecimal)) return new NumberDecimal(value);
  this.value = String(value === undefined ? 0 : value);
}
NumberDecimal.prototype.toString = function () { return this.value; };
NumberDecimal.prototype.toJSON = function () { return {$numberDecimal: this.value}; };
var Decimal128 = NumberDecimal;
function ISODate(value) { return value === undefined ? new Date() : new Date(value); }
function Timestamp(t, i) {
  if (!(this instanceof Timestamp)) return new Timestamp(t, i);
  this.t = typeof t === "object" ? t.t : Number(t || 0);
  this.i = typeof t === "object" ? t.i : Number(i || 0);
}
Timestamp.prototype.toJSON = function () { return {$timestamp: {t: this.t, i: this.i}}; };
function BinData(type, base64) {
  if (!(this instanceof BinData)) return new BinData(type, base64);
  this.type = Number(type); this.base64 = String(base64);
}
BinData.prototype.toJSON = function () {
  return {$binary: {base64: this.base64, subType: ("0" + this.type.toString(16)).slice(-2)}};
};
function UUID(value) { return BinData(4, __uuid(String(value))); }
function MinKey() { if (!(this instanceof MinKey)) return new MinKey(); }
MinKey.prototype.toJSON = function () { return {$minKey: 1}; };
function MaxKey() { if (!(this instanceof MaxKey)) return new MaxKey(); }
MaxKey.prototype.toJSON = function () { return {$maxKey: 1}; };

function __encode(value) {
  if (value instanceof Date) return {$date: {$numberLong: String(value.getTime())}};
  if (value instanceof RegExp) return {$regularExpression: {pattern: value.source, options: value.flags}};
  if (typeof value === "bigint") return {$numberLong: String(value)};
  if (Array.isArray(value)) return value.map(__encode);
  if (value && typeof value === "object") {
    if (typeof value.toJSON === "function") return value.toJSON();
    var result = Object.create(null);
    Object.keys(value).forEach(function (k) { result[k] = __encode(value[k]); });
    return result;
  }
  return value;
}
function __decode(value, relaxed) {
  if (Array.isArray(value)) return value.map(function (v) { return __decode(v, relaxed); });
  if (!value || typeof value !== "object") return value;
  var keys = Object.keys(value);
  if (keys.length === 1) {
    if ("$oid" in value) return ObjectId(value.$oid);
    if ("$numberInt" in value || "$numberDouble" in value) return Number(value[keys[0]]);
    if ("$numberLong" in value) return relaxed ? Number(value.$numberLong) : NumberLong(value.$numberLong);
    if ("$numberDecimal" in value) return NumberDecimal(value.$numberDecimal);
    if ("$date" in value) return new Date(typeof value.$date === "string" ? value.$date : Number(value.$date.$numberLong));
    if ("$regularExpression" in value) return new RegExp(value.$regularExpression.pattern, value.$regularExpression.options.replace(/x/g, ""));
    if ("$binary" in value) return BinData(parseInt(value.$binary.subType, 16), value.$binary.base64);
    if ("$timestamp" in value) return Timestamp(value.$timestamp);
    if ("$minKey" in value) return MinKey();
    if ("$maxKey" in value) return MaxKey();
  }
  var result = {};
  keys.forEach(function (k) { Object.defineProperty(result, k, {value: __decode(value[k], relaxed), writable: true, enumerable: true, configurable: true}); });
  return result;
}
var EJSON = {
  serialize: __encode,
  deserialize: function (v, opts) { return __decode(v, !!(opts && opts.relaxed)); },
  stringify: function (v, replacer, space) { return JSON.stringify(__encode(v), replacer, space); },
  parse: function (v, opts) { return __decode(JSON.parse(v), !!(opts && opts.relaxed)); }
};
function __run(database, command) { return EJSON.parse(__command(database, EJSON.stringify(command))); }
function __json(value, mode) {
  function convert(value) {
    if (typeof value === "number") {
      if (mode === "relaxed") return value;
      return Number.isInteger(value) && value >= -2147483648 && value <= 2147483647 ? {$numberInt:String(value)} : {$numberDouble:String(value)};
    }
    if (Array.isArray(value)) return value.map(convert);
    if (!value || typeof value !== "object") return value;
    if (mode === "relaxed" && ("$numberLong" in value || "$numberInt" in value || "$numberDouble" in value)) return Number(value[Object.keys(value)[0]]);
    if (mode === "relaxed" && "$date" in value && typeof value.$date !== "string") return {$date:new Date(Number(value.$date.$numberLong)).toISOString()};
    var result = Object.create(null);
    Object.keys(value).forEach(function (key) { result[key] = ["$timestamp", "$minKey", "$maxKey"].includes(key) ? value[key] : convert(value[key]); });
    return result;
  }
  return JSON.stringify(convert(__encode(value)), null, 2);
}
function __options(command, options, omit) {
  Object.keys(options || {}).forEach(function (k) { if (!(omit || []).includes(k)) command[k] = options[k]; });
  return command;
}

var config = {
  _values: {displayBatchSize: 20},
  get: function (name) { return this._values[name]; },
  set: function (name, value) {
    if (name !== "displayBatchSize" || !Number.isInteger(value) || value < 1 || value > 1000) throw new Error("displayBatchSize must be between 1 and 1000");
    this._values[name] = value; return value;
  }
};
var __liveCursors = new Set();
function __closeCursors() { Array.from(__liveCursors).forEach(function (cursor) { try { cursor.close(); } catch (_) {} }); }
class MongoCursor {
  constructor(database, command) {
    this._database = database; this._command = command;
    this._started = false; this._id = NumberLong(0); this._buffer = []; this._position = 0;
    this._collection = String(command.find || command.aggregate || command.listIndexes || "$cmd.listCollections");
  }
  _receive(result) {
    if (!result.cursor) throw new Error("command did not return a cursor");
    this._id = result.cursor.id;
    if (String(this._id) !== "0") __liveCursors.add(this); else __liveCursors.delete(this);
    if (result.cursor.ns) this._collection = result.cursor.ns.slice(this._database.length + 1);
    this._buffer = result.cursor.firstBatch || result.cursor.nextBatch || [];
    this._position = 0;
  }
  _start() {
    if (!this._started) { this._receive(__run(this._database, this._command)); this._started = true; }
  }
  hasNext() {
    this._start();
    if (this._position < this._buffer.length) return true;
    if (String(this._id) === "0") return false;
    this._receive(__run(this._database, {getMore: this._id, collection: this._collection, batchSize: 100}));
    return this._position < this._buffer.length;
  }
  next() { if (!this.hasNext()) throw new Error("cursor exhausted"); return this._buffer[this._position++]; }
  tryNext() { return this.hasNext() ? this.next() : null; }
  toArray() {
    var result = [];
    while (this.hasNext()) {
      if (result.length >= 10000) throw new Error("toArray exceeded 10000 documents; use limit() or iterate the cursor");
      result.push(this.next());
    }
    return result;
  }
  forEach(fn) { while (this.hasNext()) fn(this.next()); }
  map(fn) { return this.toArray().map(fn); }
  [Symbol.iterator]() { var self = this; return {next: function () { return self.hasNext() ? {value: self.next(), done: false} : {done: true}; }}; }
  _set(key, value) { if (this._started) throw new Error("cursor already executed"); this._command[key] = value; return this; }
  sort(value, direction) { if (typeof value === "string") { var sort = {}; sort[value] = direction === undefined ? 1 : direction; value = sort; } return this._set("sort", value); }
  limit(n) { if (n < 0) this._set("singleBatch", true); return this._set("limit", Math.abs(n)); }
  skip(n) { return this._set("skip", n); }
  projection(v) { return this._set("projection", v); }
  project(v) { return this.projection(v); }
  hint(v) { return this._set("hint", v); }
  collation(v) { return this._set("collation", v); }
  comment(v) { return this._set("comment", v); }
  maxTimeMS(v) { return this._set("maxTimeMS", v); }
  batchSize(v) { if (this._command.aggregate) { this._command.cursor.batchSize = v; return this; } return this._set("batchSize", v); }
  allowDiskUse(v) { return this._set("allowDiskUse", v === undefined ? true : v); }
  pretty() { return this; }
  count(applySkipLimit) {
    var cmd = {count: this._command.find, query: this._command.filter};
    if (!cmd.count) throw new Error("count is only available on find cursors");
    if (applySkipLimit) { if (this._command.limit) cmd.limit = this._command.limit; if (this._command.skip) cmd.skip = this._command.skip; }
    return __run(this._database, cmd).n;
  }
  itcount() { var n = 0; this.forEach(function () { n++; }); return n; }
  explain(verbosity) { return __run(this._database, {explain: this._command, verbosity: verbosity || "queryPlanner"}); }
  close() {
    if (String(this._id) !== "0") __run(this._database, {killCursors: this._collection, cursors: [this._id]});
    this._id = NumberLong(0); this._buffer = []; this._position = 0;
    __liveCursors.delete(this);
  }
  isClosed() { return this._started && String(this._id) === "0"; }
  objsLeftInBatch() { this._start(); return this._buffer.length - this._position; }
}

class Collection {
  constructor(database, name) { this._db = database; this._name = name; }
  getName() { return this._name; }
  getFullName() { return this._db.getName() + "." + this._name; }
  getDB() { return this._db; }
  find(filter, projection, options) {
    return new MongoCursor(this._db.getName(), __options({find: this._name, filter: filter || {}, batchSize: 20}, Object.assign({}, options || {}, projection === undefined ? {} : {projection: projection})));
  }
  findOne(filter, projection, options) { return this.find(filter, projection, options).limit(1).tryNext(); }
  aggregate(pipeline, options) { return new MongoCursor(this._db.getName(), __options({aggregate: this._name, pipeline: pipeline || [], cursor: {batchSize: 20}}, options)); }
  countDocuments(filter, options) {
    var pipeline = [{$match: filter || {}}];
    if (options && options.skip) pipeline.push({$skip: options.skip});
    if (options && options.limit) pipeline.push({$limit: options.limit});
    pipeline.push({$count: "n"});
    var results = this.aggregate(pipeline, __options({}, options, ["skip", "limit"])).toArray();
    return results.length ? results[0].n : 0;
  }
  estimatedDocumentCount(options) { return this._db.runCommand(__options({count: this._name}, options)).n; }
  count(filter, options) { return this._db.runCommand(__options({count: this._name, query: filter || {}}, options)).n; }
  distinct(key, query, options) { return this._db.runCommand(__options({distinct: this._name, key: key, query: query || {}}, options)).values; }
  _insert(documents, options) {
    documents.forEach(function (doc) { if (doc._id === undefined) doc._id = ObjectId(); });
    var result = this._db.runCommand(__options({insert: this._name, documents: documents, ordered: true}, options));
    __writeErrors(result); return result;
  }
  insertOne(doc, options) { this._insert([doc], options); return {acknowledged: true, insertedId: doc._id}; }
  insertMany(docs, options) {
    this._insert(docs, options); var ids = {}; docs.forEach(function (d, i) { ids[i] = d._id; });
    return {acknowledged: true, insertedIds: ids};
  }
  insert(docs, options) { return Array.isArray(docs) ? this.insertMany(docs, options) : this.insertOne(docs, options); }
  bulkWrite(operations, options) {
    var result = {acknowledged:true, insertedCount:0, matchedCount:0, modifiedCount:0, deletedCount:0, upsertedCount:0, insertedIds:{}, upsertedIds:{}};
    var failures = [], self = this;
    operations.forEach(function (operation, index) {
      if (failures.length && (!options || options.ordered !== false)) return;
      var method = Object.keys(operation)[0], spec = operation[method], value;
      try {
        var opts = Object.assign({}, options, spec); delete opts.ordered;
        ["document", "filter", "update", "replacement"].forEach(function (key) { delete opts[key]; });
        if (method === "insertOne") {
          value = self.insertOne(spec.document, opts); result.insertedCount++; result.insertedIds[index] = value.insertedId;
        } else if (method === "deleteOne" || method === "deleteMany") {
          value = self[method](spec.filter, opts); result.deletedCount += value.deletedCount;
        } else if (["updateOne", "updateMany", "replaceOne"].includes(method)) {
          value = self[method](spec.filter, method === "replaceOne" ? spec.replacement : spec.update, opts);
          result.matchedCount += value.matchedCount; result.modifiedCount += value.modifiedCount; result.upsertedCount += value.upsertedCount;
          if (value.upsertedCount) result.upsertedIds[index] = value.upsertedId;
        } else throw new Error("unsupported bulkWrite operation: " + method);
      } catch (e) { failures.push({index:index, error:String(e)}); }
    });
    if (failures.length) { var error = new Error("bulkWrite failed: " + JSON.stringify(failures)); error.result = result; throw error; }
    return result;
  }
  _update(filter, update, options, multi) {
    var spec = __options({q: filter, u: update, multi: multi, upsert: false}, options, ["writeConcern", "ordered", "bypassDocumentValidation", "comment", "let"]);
    var result = this._db.runCommand(__options({update: this._name, updates: [spec]}, options, ["upsert", "multi", "arrayFilters", "collation", "hint", "sort"]));
    __writeErrors(result);
    var upserts = result.upserted || [];
    return {acknowledged: true, matchedCount: Number(result.n) - upserts.length, modifiedCount: Number(result.nModified || 0), upsertedCount: upserts.length, upsertedId: upserts.length ? upserts[0]._id : null};
  }
  updateOne(filter, update, options) { return this._update(filter, update, options, false); }
  updateMany(filter, update, options) { return this._update(filter, update, options, true); }
  replaceOne(filter, replacement, options) { return this.updateOne(filter, replacement, options); }
  update(filter, update, options) { return this._update(filter, update, options, !!(options && options.multi)); }
  save(doc, options) { return doc._id === undefined ? this.insertOne(doc, options) : this.replaceOne({_id: doc._id}, doc, Object.assign({}, options, {upsert: true})); }
  _delete(filter, options, limit) {
    var spec = __options({q: filter || {}, limit: limit}, options, ["writeConcern", "comment", "let"]);
    var result = this._db.runCommand(__options({delete: this._name, deletes: [spec]}, options, ["collation", "hint"]));
    __writeErrors(result); return {acknowledged: true, deletedCount: Number(result.n)};
  }
  deleteOne(filter, options) { return this._delete(filter, options, 1); }
  deleteMany(filter, options) { return this._delete(filter, options, 0); }
  remove(filter, justOne) { return this._delete(filter, {}, justOne ? 1 : 0); }
  findOneAndUpdate(filter, update, options) {
    var cmd = __options({findAndModify: this._name, query: filter, update: update, new: !!(options && (options.returnDocument === "after" || options.returnNewDocument))}, options, ["returnDocument", "returnNewDocument", "projection"]);
    if (options && options.projection) cmd.fields = options.projection;
    return this._db.runCommand(cmd).value;
  }
  findOneAndReplace(filter, replacement, options) { return this.findOneAndUpdate(filter, replacement, options); }
  findOneAndDelete(filter, options) {
    var cmd = __options({findAndModify: this._name, query: filter, remove: true}, options, ["projection"]);
    if (options && options.projection) cmd.fields = options.projection;
    return this._db.runCommand(cmd).value;
  }
  findAndModify(options) { return this._db.runCommand(__options({findAndModify: this._name}, options)).value; }
  getIndexes() {
    try { return new MongoCursor(this._db.getName(), {listIndexes: this._name, cursor: {batchSize: 100}}).toArray(); }
    catch (e) { if (e.code === 26) return []; throw e; }
  }
  createIndex(keys, options) {
    var name = options && options.name || Object.keys(keys).map(function (k) { return k + "_" + keys[k]; }).join("_");
    this._db.runCommand({createIndexes: this._name, indexes: [__options({key: keys, name: name}, options)]}); return name;
  }
  createIndexes(indexes, options) { return this._db.runCommand(__options({createIndexes: this._name, indexes: indexes}, options)); }
  dropIndex(index, options) { return this._db.runCommand(__options({dropIndexes: this._name, index: index}, options)); }
  dropIndexes() { return this.dropIndex("*"); }
  drop(options) {
    try { this._db.runCommand(__options({drop: this._name}, options)); return true; }
    catch (e) { if (e.code === 26) return false; throw e; }
  }
  renameCollection(name, dropTarget) { return this._db.adminCommand({renameCollection: this.getFullName(), to: this._db.getName() + "." + name, dropTarget: !!dropTarget}); }
  stats(options) { return this._db.runCommand(__options({collStats: this._name}, options)); }
  validate(options) { return this._db.runCommand(__options({validate: this._name}, options)); }
  isCapped() { return !!this.stats().capped; }
  dataSize() { return this.stats().size; }
  storageSize() { return this.stats().storageSize; }
  totalIndexSize() { return this.stats().totalIndexSize; }
  explain(verbosity) {
    var self = this; return {find: function (f, p) { return self.find(f, p).explain(verbosity); }, aggregate: function (p, o) { return self.aggregate(p, o).explain(verbosity); }};
  }
  help() { print("find, findOne, aggregate, countDocuments, distinct, insertOne, insertMany, updateOne, updateMany, replaceOne, deleteOne, deleteMany, getIndexes, createIndex, drop, stats, explain"); }
}
function __writeErrors(result) {
  var error = result.writeErrors && result.writeErrors[0] || result.writeConcernError;
  if (error) { var e = new Error(error.errmsg || "MongoDB write failed"); e.code = error.code; throw e; }
}
class Database {
  constructor(name) { this._name = name; }
  getName() { return this._name; }
  toString() { return this._name; }
  getSiblingDB(name) { return __databaseProxy(String(name)); }
  getCollection(name) {
    var collection = new Collection(this, String(name));
    return new Proxy(collection, {get: function (target, key) {
      if (key in target || typeof key !== "string" || key === "then" || key === "toJSON") return target[key];
      return target._db.getCollection(target._name + "." + key);
    }});
  }
  runCommand(command) { return __run(this._name, typeof command === "string" ? {[command]: 1} : command); }
  adminCommand(command) { return this.getSiblingDB("admin").runCommand(command); }
  getCollectionInfos(filter, nameOnly) { return new MongoCursor(this._name, {listCollections: 1, filter: filter || {}, nameOnly: !!nameOnly, cursor: {batchSize: 100}}).toArray(); }
  getCollectionNames() { return this.getCollectionInfos({}, true).map(function (c) { return c.name; }); }
  createCollection(name, options) { return this.runCommand(__options({create: name}, options)); }
  createView(name, source, pipeline, options) { return this.createCollection(name, __options({viewOn: source, pipeline: pipeline || []}, options)); }
  dropDatabase(options) { return this.runCommand(__options({dropDatabase: 1}, options)); }
  stats(scale) { return this.runCommand({dbStats: 1, scale: scale || 1}); }
  serverStatus(options) { return this.adminCommand(__options({serverStatus: 1}, options)); }
  serverBuildInfo() { return this.adminCommand({buildInfo: 1}); }
  version() { return this.serverBuildInfo().version; }
  hostInfo() { return this.adminCommand({hostInfo: 1}); }
  currentOp(filter) { return this.adminCommand(__options({currentOp: 1}, filter)); }
  killOp(opid) { return this.adminCommand({killOp: 1, op: opid}); }
  getUsers(options) { return this.runCommand(__options({usersInfo: 1}, options)).users; }
  getUser(name, options) { var users = this.runCommand(__options({usersInfo: name}, options)).users; return users.length ? users[0] : null; }
  createUser(user, options) { var cmd = __options({createUser: user.user}, user, ["user"]); return this.runCommand(__options(cmd, options)); }
  updateUser(name, options) { return this.runCommand(__options({updateUser: name}, options)); }
  dropUser(name, options) { return this.runCommand(__options({dropUser: name}, options)); }
  getRoles(options) { return this.runCommand(__options({rolesInfo: 1}, options)).roles; }
  getRole(name, options) { return this.runCommand(__options({rolesInfo: name}, options)).roles[0] || null; }
  createRole(role, options) { return this.runCommand(__options(__options({createRole: role.role}, role, ["role"]), options)); }
  dropRole(name, options) { return this.runCommand(__options({dropRole: name}, options)); }
  grantRolesToUser(name, roles, options) { return this.runCommand(__options({grantRolesToUser: name, roles: roles}, options)); }
  revokeRolesFromUser(name, roles, options) { return this.runCommand(__options({revokeRolesFromUser: name, roles: roles}, options)); }
  getMongo() { return __connection; }
  help() { print("getName, getSiblingDB, getCollection, getCollectionNames, runCommand, adminCommand, createCollection, dropDatabase, stats, serverStatus, getUsers, getRoles"); }
}
function __databaseProxy(name) {
  return new Proxy(new Database(name), {get: function (target, key) {
    if (key in target || typeof key !== "string" || key === "then" || key === "toJSON") return target[key];
    return target.getCollection(key);
  }});
}
var __connection = {
  getDB: __databaseProxy,
  getDBNames: function () { return __run("admin", {listDatabases: 1, nameOnly: true}).databases.map(function (d) { return d.name; }); },
  getDBs: function () { return __run("admin", {listDatabases: 1}); }
};
var db = __databaseProxy(__database);
var rs = {
  status: function () { return db.adminCommand({replSetGetStatus: 1}); },
  conf: function () { return db.adminCommand({replSetGetConfig: 1}).config; },
  initiate: function (config) { return db.adminCommand({replSetInitiate: config || {}}); },
  reconfig: function (config, options) { return db.adminCommand(__options({replSetReconfig: config}, options)); },
  stepDown: function (seconds, catchUp) { return db.adminCommand({replSetStepDown: seconds || 60, secondaryCatchUpPeriodSecs: catchUp || 10}); }
};
var sh = {
  status: function () { return db.getSiblingDB("config").getCollection("shards").find().toArray(); },
  addShard: function (host, name) { return db.adminCommand(__options({addShard: host}, name ? {name: name} : {})); },
  enableSharding: function (name) { return db.adminCommand({enableSharding: name}); },
  shardCollection: function (name, key, unique) { return db.adminCommand({shardCollection: name, key: key, unique: !!unique}); }
};

function __format(value, depth) {
  depth = depth || 0;
  if (depth > 30) return "[Object]";
  if (value instanceof ObjectId) return "ObjectId('" + value.str + "')";
  if (value instanceof NumberLong) return "Long('" + value.value + "')";
  if (value instanceof NumberInt) return String(value.value);
  if (value instanceof NumberDecimal) return "Decimal128('" + value.value + "')";
  if (value instanceof Timestamp) return "Timestamp({ t: " + value.t + ", i: " + value.i + " })";
  if (value instanceof BinData) return "BinData(" + value.type + ", '" + value.base64 + "')";
  if (value instanceof MinKey) return "MinKey()";
  if (value instanceof MaxKey) return "MaxKey()";
  if (value instanceof Date) return "ISODate('" + value.toISOString() + "')";
  if (value instanceof RegExp) return String(value);
  if (value instanceof Collection) return value.getFullName();
  if (value instanceof Database) return value.getName();
  if (typeof value === "string") return "'" + value.replace(/\\/g, "\\\\").replace(/'/g, "\\'").replace(/\n/g, "\\n").replace(/\r/g, "\\r") + "'";
  if (Array.isArray(value)) return value.length ? "[\n" + value.map(function (v) { return "  " + __format(v, depth + 1); }).join(",\n") + "\n]" : "[]";
  if (value && typeof value === "object") return "{ " + Object.keys(value).map(function (k) { return (/^[a-zA-Z_$][\w$]*$/.test(k) ? k : JSON.stringify(k)) + ": " + __format(value[k], depth + 1); }).join(", ") + " }";
  return String(value);
}
function print() { __print(Array.from(arguments).map(function (v) { return typeof v === "string" ? v : __format(v); }).join(" ")); }
function printjson(v) { __print(__format(v)); }
var printjsononeline = printjson;
var tojson = __format;
var tojsononeline = __format;
var console = {log: print, info: print, warn: print, error: print, dir: printjson};
function quit(code) { __quit(code || 0); }
var exit = quit;
function help() { print("Mongo Shell mode: show dbs, use <db>, show collections, db.<collection>.find(), db.runCommand(), rs.status(), quit().\nCursor: sort, skip, limit, next, hasNext, toArray, forEach. Type it for the next result page.\nJavaScript and BSON helpers are available. Node.js APIs, sessions and client-side encryption are not supported."); }
function assert(value, message) { if (!value) throw new Error(message || "assertion failed"); }
assert.eq = function (expected, actual, message) { if (EJSON.stringify(expected) !== EJSON.stringify(actual)) throw new Error(message || "values are not equal"); };
assert.commandWorked = function (result) { if (Number(result.ok) !== 1) throw new Error(result.errmsg || "command failed"); return result; };
function __show(what) {
  if (what === "dbs" || what === "databases") __connection.getDBNames().forEach(function (name) { print(name); });
  else if (what === "collections" || what === "tables") db.getCollectionNames().forEach(function (name) { print(name); });
  else if (what === "users") printjson(db.getUsers());
  else if (what === "roles") printjson(db.getRoles());
  else if (what === "profile") __display(db.getCollection("system.profile").find().sort({$natural: -1}).limit(5), "");
  else if (what === "logs") printjson(db.adminCommand({getLog: "*"}));
}
var __lastCursor;
function __display(value, mode) {
  if (value instanceof MongoCursor) {
    __lastCursor = value;
    var page = [], size = config.get("displayBatchSize");
    while (page.length < size && value.hasNext()) page.push(value.next());
    __print(mode ? __json(page, mode) : __format(page));
    if (!mode && value.hasNext()) print('Type "it" for more');
  } else if (value !== undefined) {
    __print(mode ? __json(value, mode) : __format(value));
  }
}
function __more() { if (__lastCursor) __display(__lastCursor, ""); }
