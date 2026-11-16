import { SuperComponent, wxComponent } from '../common/src/index';
import config from '../common/config';
import props from './props';
import { styles, addUnit, getRect } from '../common/utils';

const { prefix } = config;
const name = `${prefix}-icon`;

class Icon extends SuperComponent {
  constructor() {
    super(...arguments);
    this.externalClasses = [`${prefix}-class`];
    this.properties = props;
    this.data = {
      componentPrefix: prefix,
      classPrefix: name,
      isImage: false,
      iconStyle: undefined,
    };
    this.observers = {
      'name, color, size, style'() { this.setIconStyle(); },
    };
    this.methods = {
      onTap(event) { this.triggerEvent('click', event.detail); },
      setIconStyle() {
        const { name, color, size, classPrefix } = this.data;
        const isImage = name.indexOf('/') !== -1;
        const unitSize = size !== null && size !== '' ? addUnit(size) : undefined;
        const colorStyle = color ? { color } : {};
        const sizeStyle = size ? { 'font-size': unitSize } : {};
        const iconStyle = Object.assign(Object.assign({}, colorStyle), sizeStyle);

        // The vendored mini-program component does not ship tslib; native async
        // syntax keeps its existing behavior without an external runtime helper.
        this.setData({ isImage }, async () => {
          if (isImage) {
            let imageSize = unitSize;
            if (!imageSize) {
              await getRect(this, `.${classPrefix}`).then(rect => {
                imageSize = addUnit(rect == null ? undefined : rect.height);
              }).catch(() => {});
            }
            iconStyle.width = imageSize;
            iconStyle.height = imageSize;
          }
          this.setData({ iconStyle: styles(iconStyle) });
        });
      },
    };
  }
}

// wxComponent registers the class and returns no replacement constructor.
wxComponent()(Icon);
export default Icon;
