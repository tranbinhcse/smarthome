// Ảnh nền từng trang (bản ngày + bản đêm) và vị trí nhãn trên ảnh, đơn vị % theo chiều rộng / cao của ảnh: [x, y].
// Thay ảnh: bỏ file .webp vào web/images rồi sửa đường dẫn ở đây. Tìm toạ độ nhãn: mở /?demo&vung, chạm lên ảnh để xem %.
const SCENE = {
  floorNames: {1: 'Tầng trệt', 2: 'Tầng 1', 3: 'Tầng tum'},
  // Cả nhà: ảnh phủ kín màn hình; nhãn là từng tầng.
  home: {day: '/images/tongquan-sang.webp', night: '/images/tongquan-toi.webp', w: 1448, h: 1086,
    chips: {3: [17, 33], 2: [14, 50], 1: [16, 67]}},
  // Từng tầng: ảnh đặt trong vùng trống giữa các widget (nền là chính ảnh đó làm mờ); nhãn là từng phòng.
  floors: {
    1: {day: '/images/tang-tret-sang.webp', night: '/images/tang-tret-toi.webp', w: 1672, h: 941,
      chips: {santruoc: [24, 60], cafe: [39, 47], wc: [58, 22], kho: [73, 28], bep: [70, 64], sansau: [89, 45]}},
    2: {day: '/images/tang-1-sang.webp', night: '/images/tang-1-toi.webp', w: 1672, h: 941,
      chips: {lamviec: [17, 54], docsach: [53, 64], ngu1: [77, 68]}},
    3: {day: '/images/tang-tum-sang.webp', night: '/images/tang-tum-toi.webp', w: 1672, h: 941,
      chips: {kythuat: [22, 67], gieng_troi: [62, 66]}},
  },
  // Ảnh riêng từng phòng (16:9). Phòng chưa có ảnh: dùng ảnh tầng, nhãn phòng đó được làm nổi.
  rooms: {santruoc: '/img/phong-santruoc.webp', cafe: '/img/phong-cafe.webp', bep: '/img/phong-bep.webp'},
};
