import React, { useState, useEffect, useCallback } from 'react';
import { FiChevronRight, FiChevronDown, FiPlus, FiTrash2, FiEdit2, FiCheck, FiX } from 'react-icons/fi';
import './json-tree-editor.scss';

interface JSONNode {
    key: string;
    value: any;
    type: 'object' | 'array' | 'string' | 'number' | 'boolean' | 'null' | 'placeholder';
    path: string;
    children?: JSONNode[];
}

interface JSONTreeEditorProps {
    value: any;
    onChange: (value: any) => void;
    disabled?: boolean;
}

const JSONTreeEditor: React.FC<JSONTreeEditorProps> = ({ value, onChange, disabled = false }) => {
    const [tree, setTree] = useState<JSONNode[]>([]);
    const [expandedPaths, setExpandedPaths] = useState<Set<string>>(new Set());
    const [editingPath, setEditingPath] = useState<string | null>(null);
    const [editingValue, setEditingValue] = useState<string>('');

    // 解析JSON为树形结构
    const parseToTree = useCallback((obj: any, parentPath: string = ''): JSONNode[] => {
        if (obj === null || obj === undefined) {
            return [{
                key: '',
                value: null,
                type: 'null',
                path: parentPath
            }];
        }

        if (typeof obj === 'string' && (obj === '{{prompt}}' || obj === '{{image}}')) {
            return [{
                key: '',
                value: obj,
                type: 'placeholder',
                path: parentPath
            }];
        }

        if (Array.isArray(obj)) {
            return obj.map((item, index) => {
                const path = parentPath ? `${parentPath}[${index}]` : `[${index}]`;
                const node: JSONNode = {
                    key: `[${index}]`,
                    value: item,
                    type: getValueType(item),
                    path
                };

                if (node.type === 'object' || node.type === 'array') {
                    node.children = parseToTree(item, path);
                }

                return node;
            });
        }

        if (typeof obj === 'object') {
            return Object.keys(obj).map(key => {
                const path = parentPath ? `${parentPath}.${key}` : key;
                const node: JSONNode = {
                    key,
                    value: obj[key],
                    type: getValueType(obj[key]),
                    path
                };

                if (node.type === 'object' || node.type === 'array') {
                    node.children = parseToTree(obj[key], path);
                }

                return node;
            });
        }

        return [{
            key: '',
            value: obj,
            type: getValueType(obj),
            path: parentPath
        }];
    }, []);

    const getValueType = (val: any): JSONNode['type'] => {
        if (val === null || val === undefined) return 'null';
        if (Array.isArray(val)) return 'array';
        if (typeof val === 'object') return 'object';
        if (typeof val === 'string' && (val === '{{prompt}}' || val === '{{image}}')) return 'placeholder';
        return typeof val as 'string' | 'number' | 'boolean';
    };

    // 将树形结构转换回JSON
    const treeToJSON = useCallback((nodes: JSONNode[]): any => {
        if (nodes.length === 0) return null;

        // 如果是数组节点
        if (nodes[0]?.key.startsWith('[')) {
            return nodes.map(node => {
                if (node.type === 'object' || node.type === 'array') {
                    return treeToJSON(node.children || []);
                }
                return node.value;
            });
        }

        // 如果是对象节点
        const result: any = {};
        nodes.forEach(node => {
            if (node.type === 'object' || node.type === 'array') {
                result[node.key] = treeToJSON(node.children || []);
            } else {
                result[node.key] = node.value;
            }
        });
        return result;
    }, []);

    // 初始化树形结构
    useEffect(() => {
        try {
            const parsed = typeof value === 'string' ? JSON.parse(value) : value;
            const treeData = parseToTree(parsed);
            setTree(treeData);
            // 默认展开所有节点
            const allPaths = new Set<string>();
            const collectPaths = (nodes: JSONNode[]) => {
                nodes.forEach(node => {
                    if (node.type === 'object' || node.type === 'array') {
                        allPaths.add(node.path);
                        if (node.children) {
                            collectPaths(node.children);
                        }
                    }
                });
            };
            collectPaths(treeData);
            setExpandedPaths(allPaths);
        } catch (error) {
            console.error('解析JSON失败:', error);
            setTree([]);
        }
    }, [value, parseToTree]);

    // 切换展开/折叠
    const toggleExpand = (path: string) => {
        const newExpanded = new Set(expandedPaths);
        if (newExpanded.has(path)) {
            newExpanded.delete(path);
        } else {
            newExpanded.add(path);
        }
        setExpandedPaths(newExpanded);
    };

    // 开始编辑
    const startEdit = (node: JSONNode) => {
        if (disabled) return;
        setEditingPath(node.path);
        if (node.type === 'string' || node.type === 'number' || node.type === 'boolean' || node.type === 'placeholder') {
            setEditingValue(String(node.value));
        } else if (node.type === 'null') {
            setEditingValue('null');
        } else {
            setEditingValue(JSON.stringify(node.value, null, 2));
        }
    };

    // 保存编辑
    const saveEdit = (path: string) => {
        const updatedTree = updateNodeValue([...tree], path, editingValue);
        setTree(updatedTree);
        const json = treeToJSON(updatedTree);
        onChange(json);
        setEditingPath(null);
        setEditingValue('');
    };

    // 取消编辑
    const cancelEdit = () => {
        setEditingPath(null);
        setEditingValue('');
    };

    // 更新节点值
    const updateNodeValue = (nodes: JSONNode[], path: string, newValue: string): JSONNode[] => {
        return nodes.map(node => {
            if (node.path === path) {
                let parsedValue: any = newValue;
                if (node.type === 'number') {
                    parsedValue = Number(newValue);
                } else if (node.type === 'boolean') {
                    parsedValue = newValue === 'true';
                } else if (node.type === 'null') {
                    parsedValue = null;
                } else if (newValue === '{{prompt}}' || newValue === '{{image}}') {
                    parsedValue = newValue;
                    node.type = 'placeholder';
                }

                const updatedNode = { ...node, value: parsedValue };
                if (node.type === 'object' || node.type === 'array') {
                    try {
                        const parsed = typeof parsedValue === 'string' ? JSON.parse(parsedValue) : parsedValue;
                        updatedNode.children = parseToTree(parsed, path);
                    } catch {
                        // 如果解析失败，保持原样
                    }
                }
                return updatedNode;
            }
            if (node.children) {
                return { ...node, children: updateNodeValue(node.children, path, newValue) };
            }
            return node;
        });
    };

    // 删除节点
    const deleteNode = (path: string) => {
        if (disabled) return;
        const updatedTree = deleteNodeFromTree([...tree], path);
        setTree(updatedTree);
        const json = treeToJSON(updatedTree);
        onChange(json);
    };

    // 从树中删除节点
    const deleteNodeFromTree = (nodes: JSONNode[], path: string): JSONNode[] => {
        return nodes.filter(node => {
            if (node.path === path) {
                return false;
            }
            if (node.children) {
                node.children = deleteNodeFromTree(node.children, path);
            }
            return true;
        });
    };

    // 添加新节点
    const addNode = (parentPath: string, isArray: boolean = false) => {
        if (disabled) return;
        const newKey = isArray ? `[${tree.length}]` : 'newKey';
        const newPath = parentPath ? `${parentPath}.${newKey}` : newKey;
        const newNode: JSONNode = {
            key: newKey,
            value: '',
            type: 'string',
            path: newPath
        };

        const updatedTree = addNodeToTree([...tree], parentPath, newNode, isArray);
        setTree(updatedTree);
        const json = treeToJSON(updatedTree);
        onChange(json);
        setEditingPath(newPath);
        setEditingValue('');
    };

    // 向树中添加节点
    const addNodeToTree = (nodes: JSONNode[], parentPath: string, newNode: JSONNode, isArray: boolean): JSONNode[] => {
        if (!parentPath) {
            return [...nodes, newNode];
        }

        return nodes.map(node => {
            if (node.path === parentPath) {
                if (!node.children) {
                    node.children = [];
                }
                return { ...node, children: [...node.children, newNode] };
            }
            if (node.children) {
                return { ...node, children: addNodeToTree(node.children, parentPath, newNode, isArray) };
            }
            return node;
        });
    };

    // 插入占位符
    const insertPlaceholder = (path: string, placeholder: '{{prompt}}' | '{{image}}') => {
        if (disabled) return;
        const updatedTree = updateNodeValue([...tree], path, placeholder);
        setTree(updatedTree);
        const json = treeToJSON(updatedTree);
        onChange(json);
    };

    // 渲染节点
    const renderNode = (node: JSONNode, level: number = 0): React.ReactNode => {
        const isExpanded = expandedPaths.has(node.path);
        const isEditing = editingPath === node.path;
        const isPlaceholder = node.type === 'placeholder';
        const isComplex = node.type === 'object' || node.type === 'array';

        return (
            <div key={node.path} className="json-node" style={{ marginLeft: `${level * 20}px` }}>
                <div className="json-node-header">
                    {isComplex && (
                        <button
                            className="expand-btn"
                            onClick={() => toggleExpand(node.path)}
                            disabled={disabled}
                        >
                            {isExpanded ? <FiChevronDown /> : <FiChevronRight />}
                        </button>
                    )}
                    {!isComplex && <span className="expand-spacer" />}

                    <span className="json-key">
                        {node.key && (
                            <>
                                <span className="key-name">{node.key}</span>
                                <span className="key-separator">:</span>
                            </>
                        )}
                    </span>

                    {isEditing ? (
                        <div className="edit-controls">
                            <input
                                type="text"
                                value={editingValue}
                                onChange={(e) => setEditingValue(e.target.value)}
                                onKeyDown={(e) => {
                                    if (e.key === 'Enter') {
                                        saveEdit(node.path);
                                    } else if (e.key === 'Escape') {
                                        cancelEdit();
                                    }
                                }}
                                autoFocus
                                className="edit-input"
                            />
                            <button className="icon-btn save-btn" onClick={() => saveEdit(node.path)}>
                                <FiCheck />
                            </button>
                            <button className="icon-btn cancel-btn" onClick={cancelEdit}>
                                <FiX />
                            </button>
                        </div>
                    ) : (
                        <div className="json-value-container">
                            <span
                                className={`json-value ${node.type} ${isPlaceholder ? 'placeholder' : ''}`}
                                onClick={() => !disabled && startEdit(node)}
                            >
                                {isPlaceholder ? node.value : formatValue(node.value, node.type)}
                            </span>
                            {!disabled && (
                                <div className="node-actions">
                                    {(node.type === 'string' || node.type === 'placeholder' || node.type === 'null') && (
                                        <>
                                            <button
                                                className="icon-btn placeholder-btn"
                                                onClick={() => insertPlaceholder(node.path, '{{prompt}}')}
                                                title="插入提示词占位符"
                                            >
                                                提示词
                                            </button>
                                            <button
                                                className="icon-btn placeholder-btn"
                                                onClick={() => insertPlaceholder(node.path, '{{image}}')}
                                                title="插入图片占位符"
                                            >
                                                图片
                                            </button>
                                        </>
                                    )}
                                    <button
                                        className="icon-btn"
                                        onClick={() => startEdit(node)}
                                        title="编辑"
                                    >
                                        <FiEdit2 />
                                    </button>
                                    <button
                                        className="icon-btn delete-btn"
                                        onClick={() => deleteNode(node.path)}
                                        title="删除"
                                    >
                                        <FiTrash2 />
                                    </button>
                                </div>
                            )}
                        </div>
                    )}

                    {isComplex && (
                        <button
                            className="add-btn"
                            onClick={() => addNode(node.path, node.type === 'array')}
                            disabled={disabled}
                            title="添加子节点"
                        >
                            <FiPlus />
                        </button>
                    )}
                </div>

                {isComplex && isExpanded && node.children && (
                    <div className="json-children">
                        {node.children.map(child => renderNode(child, level + 1))}
                    </div>
                )}
            </div>
        );
    };

    const formatValue = (val: any, type: JSONNode['type']): string => {
        if (type === 'null') return 'null';
        if (type === 'string') return `"${val}"`;
        if (type === 'boolean') return String(val);
        if (type === 'number') return String(val);
        if (type === 'object') return '{...}';
        if (type === 'array') return '[...]';
        return String(val);
    };

    return (
        <div className="json-tree-editor">
            <div className="json-tree-toolbar">
                <button
                    className="toolbar-btn"
                    onClick={() => addNode('', false)}
                    disabled={disabled}
                >
                    <FiPlus />
                    添加根节点
                </button>
            </div>
            <div className="json-tree-content">
                {tree.length === 0 ? (
                    <div className="empty-tree">暂无数据，请粘贴JSON或添加节点</div>
                ) : (
                    tree.map(node => renderNode(node, 0))
                )}
            </div>
        </div>
    );
};

export default JSONTreeEditor;
